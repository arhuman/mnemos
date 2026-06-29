package eval

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/embed"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/storage"
)

// defaultK is the held-out evaluation depth (matches the plan's Recall@12 and
// config search.default_limit). Overridable per run via Options.K.
const defaultK = 12

// evalCollection is the fixed collection name used for the ephemeral corpus. It
// is internal to the eval run and never touches the user's real database.
const evalCollection = "eval"

// Options configures one evaluation run. Bundle is the path to the OKF bundle.
// K is the retrieval depth (defaults to 12). Include/Exclude/Chunking mirror the
// ingest configuration so the held-out copy is indexed exactly like production.
type Options struct {
	Bundle   string
	K        int
	Include  []string
	Exclude  []string
	Chunking chunk.Config
	// Semantic, when true, evaluates the hybrid (bm25 + vector) retriever instead
	// of the lexical engine, so the run can be compared against the FTS baseline.
	Semantic bool
	// ModelDir is the embedding model directory used when Semantic is true.
	ModelDir string
}

// Run executes the full held-out evaluation: derive example-query pairs, build a
// held-out copy with those queries stripped, ingest the copy into an ephemeral
// SQLite database, search each query, and compute doc-level metrics. The user's
// real database is never touched — everything happens under a temp directory
// that is removed on return. The Retriever is the FTS5 engine over the temp DB.
func Run(ctx context.Context, logger *slog.Logger, opts Options) (Metrics, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	k := opts.K
	if k <= 0 {
		k = defaultK
	}

	pairs, err := extractPairs(opts.Bundle)
	if err != nil {
		return Metrics{}, fmt.Errorf("eval: extract pairs: %w", err)
	}
	logger.Info("eval derived example queries", "pairs", len(pairs), "bundle", opts.Bundle)
	if len(pairs) == 0 {
		return Metrics{K: k}, nil
	}

	work, err := os.MkdirTemp("", "mnemos-eval-*")
	if err != nil {
		return Metrics{}, fmt.Errorf("eval: temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(work) }()

	heldOut := filepath.Join(work, "corpus")
	if err = os.MkdirAll(heldOut, 0o750); err != nil {
		return Metrics{}, fmt.Errorf("eval: held-out dir: %w", err)
	}
	if _, err = buildHeldOut(opts.Bundle, heldOut, pairs); err != nil {
		return Metrics{}, fmt.Errorf("eval: build held-out: %w", err)
	}

	dbPath := filepath.Join(work, "eval.db")
	db, err := storage.Open(ctx, dbPath)
	if err != nil {
		return Metrics{}, fmt.Errorf("eval: open temp db: %w", err)
	}
	defer func() { _ = db.Close() }()
	if err = storage.Migrate(db); err != nil {
		return Metrics{}, fmt.Errorf("eval: migrate temp db: %w", err)
	}

	pipeline := ingest.New(db, logger)
	if _, err = pipeline.Run(ctx, ingest.Options{
		Root:       heldOut,
		Collection: evalCollection,
		Rules:      ingest.Rules{Include: opts.Include, Exclude: opts.Exclude},
		Chunking:   opts.Chunking,
	}); err != nil {
		return Metrics{}, fmt.Errorf("eval: ingest held-out: %w", err)
	}

	retriever, err := buildRetriever(ctx, db, logger, opts)
	if err != nil {
		return Metrics{}, err
	}

	cases := make([]evalCase, 0, len(pairs))
	for _, p := range pairs {
		results, err := retriever.Search(ctx, search.Query{Text: p.queryText, Limit: k})
		if err != nil {
			// An unsearchable query (e.g. pure punctuation that sanitizes to
			// empty) counts as a miss rather than aborting the whole run.
			if errors.Is(err, search.ErrEmptyQuery) {
				logger.Debug("eval skipping empty query", "uri", p.expectedURI)
				cases = append(cases, evalCase{pair: p})

				continue
			}

			return Metrics{}, fmt.Errorf("eval: search %q: %w", p.expectedURI, err)
		}
		cases = append(cases, evalCase{pair: p, results: results})
	}

	return compute(cases, k), nil
}

// buildRetriever returns the retriever the eval run scores. The default is the
// lexical FTS engine (the Phase 1 baseline). When opts.Semantic is set it embeds
// the held-out corpus into the temp database and returns a hybrid retriever, so
// the same metrics can be recomputed to check semantic beats the FTS baseline.
func buildRetriever(ctx context.Context, db *sql.DB, logger *slog.Logger, opts Options) (search.Retriever, error) {
	engine := search.NewEngine(db, logger)
	if !opts.Semantic {
		return engine, nil
	}
	if !embed.Supported {
		return nil, errors.New("eval: semantic mode requires an embed-tagged build")
	}
	emb, err := embed.New(opts.ModelDir)
	if err != nil {
		return nil, fmt.Errorf("eval: load embedder: %w", err)
	}
	if _, err := search.Reindex(ctx, db, emb, logger); err != nil {
		return nil, fmt.Errorf("eval: embed held-out corpus: %w", err)
	}
	vector := search.NewVectorRetriever(db, emb, logger)

	return search.NewHybridRetriever(engine, vector, logger), nil
}

// Report runs an evaluation and renders the metrics table to out, loading the
// baseline at baselinePath for delta annotation when present and saving the
// fresh metrics back when save is true. It returns the computed Metrics so
// callers can reuse them.
func Report(ctx context.Context, logger *slog.Logger, out io.Writer, opts Options, baselinePath string, save bool) (Metrics, error) {
	m, err := Run(ctx, logger, opts)
	if err != nil {
		return Metrics{}, err
	}

	baseline, err := loadBaseline(baselinePath)
	if err != nil {
		return Metrics{}, err
	}
	writeReport(out, m, baseline)

	if save {
		if err := saveBaseline(baselinePath, m); err != nil {
			return Metrics{}, err
		}
		_, _ = fmt.Fprintf(out, "saved baseline: %s\n", baselinePath)
	}

	return m, nil
}
