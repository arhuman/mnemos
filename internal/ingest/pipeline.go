// Package ingest implements the V0 ingestion pipeline: discover files, hash
// them, skip unchanged ones, parse, chunk, and write documents/chunks/links and
// an event in a single transaction per document. Parse and chunk run in a
// bounded worker pool; all writes are funneled through one writer because the
// SQLite handle is single-connection (one writer).
package ingest

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"runtime"

	"golang.org/x/sync/errgroup"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/model"
)

// Options configures a single ingest run.
type Options struct {
	Root string
	// URIBase is the directory document URIs are made relative to. Leave empty to
	// use Root (scan-root-relative URIs); set it to the knowledge-base root so a
	// subtree ingest still mints kb-relative URIs.
	URIBase    string
	Collection string
	Rules      Rules
	Chunking   chunk.Config
}

// Rules mirrors the config glob sets that drive file selection.
type Rules struct {
	Include         []string
	Exclude         []string
	SecurityExclude []string
}

// Summary reports the outcome of an ingest run.
type Summary struct {
	FilesScanned  int
	FilesIngested int
	FilesSkipped  int
	ChunksWritten int
}

// defaultMaxFileBytes is the built-in per-file size cap applied when a caller
// does not configure one. It mirrors the [indexing].max_file_bytes default.
const defaultMaxFileBytes int64 = 4 << 20 // 4 MiB

// Pipeline ingests files into the store. It holds the dependencies the run
// needs; construct one per command invocation.
type Pipeline struct {
	db           *sql.DB
	logger       *slog.Logger
	tc           chunk.TokenCounter
	maxFileBytes int64
}

// Option customizes a Pipeline at construction.
type Option func(*Pipeline)

// WithMaxFileBytes sets the per-file size cap. A file larger than n is skipped
// (with a warning) instead of being read into memory. n <= 0 disables the cap.
func WithMaxFileBytes(n int64) Option {
	return func(p *Pipeline) { p.maxFileBytes = n }
}

// New builds a Pipeline over db using the default offline token estimator and
// the default file-size cap. Pass WithMaxFileBytes to override the cap.
func New(db *sql.DB, logger *slog.Logger, opts ...Option) *Pipeline {
	p := &Pipeline{db: db, logger: logger, tc: chunk.WordEstimator{}, maxFileBytes: defaultMaxFileBytes}
	for _, opt := range opts {
		opt(p)
	}

	return p
}

// result carries the outcome of preparing one file to the single writer. A
// skipped file (unchanged hash) sets skip and leaves doc/chunks empty.
type result struct {
	doc    model.Document
	chunks []model.Chunk
	links  []model.Link
	skip   bool
}

// writeTally is the running outcome the single writer goroutine accumulates and
// hands back to Run once the result channel closes.
type writeTally struct {
	ingested int
	skipped  int
	chunks   int
	err      error
}

// drainWrites consumes prepared results from in until it closes, applying every
// write through p.write and accumulating the tally. It owns all DB writes and
// all counters, so Run's totals are race-free by construction. A panic in
// p.write is recovered and surfaced as the tally error rather than leaving Run
// to deadlock on an unsent result.
func (p *Pipeline) drainWrites(ctx context.Context, in <-chan result, out chan<- writeTally) {
	var t writeTally
	defer func() {
		if rec := recover(); rec != nil {
			t.err = fmt.Errorf("ingest: writer goroutine panicked: %v", rec)
			out <- t
		}
	}()
	for r := range in {
		if t.err != nil {
			continue // drain remaining results after a failure
		}
		if r.skip {
			t.skipped++

			continue
		}
		if err := p.write(ctx, r); err != nil {
			t.err = err

			continue
		}
		t.ingested++
		t.chunks += len(r.chunks)
	}
	out <- t
}

// Run executes the pipeline and returns a Summary. Discovery and hashing run on
// the calling goroutine; parse+chunk run in a bounded worker pool; every result
// flows through one writer goroutine that owns all DB writes and all tallying,
// so the counters are race-free by construction.
func (p *Pipeline) Run(ctx context.Context, opts Options) (Summary, error) {
	files, err := scan(opts.Root, opts.URIBase, scanRules{
		include:         opts.Rules.Include,
		exclude:         opts.Rules.Exclude,
		securityExclude: opts.Rules.SecurityExclude,
	})
	if err != nil {
		return Summary{}, err
	}

	summary := Summary{FilesScanned: len(files)}
	p.logger.Info("ingest discovered files", "count", len(files), "root", opts.Root)

	resCh := make(chan result)
	writeDone := make(chan writeTally, 1)

	// Single writer: owns every DB write and the running totals.
	go p.drainWrites(ctx, resCh, writeDone)

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.GOMAXPROCS(0))

	for _, f := range files {
		g.Go(func() error {
			r, err := p.prepare(gctx, f, opts)
			if err != nil {
				return err
			}
			select {
			case resCh <- r:
				return nil
			case <-gctx.Done():
				return gctx.Err()
			}
		})
	}

	prepErr := g.Wait()
	close(resCh)
	done := <-writeDone

	if done.err != nil {
		return summary, done.err
	}
	if prepErr != nil {
		return summary, prepErr
	}

	summary.FilesIngested = done.ingested
	summary.FilesSkipped = done.skipped
	summary.ChunksWritten = done.chunks

	return summary, nil
}
