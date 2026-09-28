package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/storage"
)

// errReindexWrite tags a reindex failure that happened while persisting a
// prepared document, so a batch caller can tell a broken store (fatal) apart
// from a single unreadable file (skippable).
var errReindexWrite = errors.New("ingest: reindex write")

// ReindexContentSummary reports the outcome of a content reindex.
type ReindexContentSummary struct {
	// Documents is how many indexed documents were considered.
	Documents int
	// Reindexed is how many were re-parsed and rewritten.
	Reindexed int
	// Skipped is how many prepare declined (binary/oversize/unparseable on disk).
	Skipped int
	// Missing is how many had no readable backing file (unreadable, e.g. a
	// permission error). A file that is simply gone is evicted by the reconcile
	// pass instead and counted in Removed.
	Missing int
	// Removed is how many documents were evicted because their backing file no
	// longer exists on disk.
	Removed int
	// Chunks is the total chunks rewritten across reindexed documents.
	Chunks int
}

// ReindexContent re-parses and rewrites every already-indexed document from its
// on-disk file, bypassing the content-hash skip, so a parser or schema change
// (e.g. a corrected title derivation) propagates to the whole index without
// editing files. It walks the documents table rather than the filesystem, so each
// document keeps its stored collection and only indexed documents are touched.
//
// A document whose file no longer exists is evicted, so a reindex converges the
// index onto what is on disk instead of leaving a deleted file citable (#42). An
// unreadable-but-present file is left in place with a warning. If kbRoot itself is
// unreadable the whole pass fails without evicting anything, because absence
// below an unreadable root proves nothing.
//
// Rewriting a document's chunks cascades away its stored embeddings (they key on
// chunk id); callers that use semantic search should run `reindex --embeddings`
// afterwards.
func (p *Pipeline) ReindexContent(ctx context.Context, kbRoot string, cfg chunk.Config) (ReindexContentSummary, error) {
	// Reconcile first so a deleted file is evicted rather than re-read, warned
	// about, and left citable.
	rec, err := Reconcile(ctx, p.db, kbRoot, kbRoot, "")
	if err != nil {
		return ReindexContentSummary{}, err
	}
	for _, uri := range rec.URIs {
		p.logger.Info("reindex removed vanished document", "uri", uri)
	}

	docs, err := storage.ListDocuments(ctx, p.db, storage.ListFilter{})
	if err != nil {
		return ReindexContentSummary{}, fmt.Errorf("ingest: reindex list documents: %w", err)
	}

	sum := ReindexContentSummary{Documents: len(docs), Removed: rec.Removed}
	for _, d := range docs {
		if err := ctx.Err(); err != nil {
			return sum, err
		}
		abs := filepath.Join(kbRoot, filepath.FromSlash(d.URI))
		r, err := p.reindexFile(ctx, abs, d.URI, d.Collection, cfg, true)
		if err != nil {
			if errors.Is(err, errReindexWrite) {
				return sum, err
			}
			// A vanished or unreadable file must not abort the whole pass; leave its
			// row in place and keep going.
			p.logger.Warn("reindex skip: file unreadable", "uri", d.URI, "error", err)
			sum.Missing++

			continue
		}
		if r.skip {
			sum.Skipped++

			continue
		}
		sum.Reindexed++
		sum.Chunks += len(r.chunks)
	}

	return sum, nil
}

// reindexFile force-prepares one file and, unless prepare declined it, writes
// it. Forcing bypasses the content-hash skip, so an unchanged file is still
// re-parsed and rewritten. A prepare failure is returned unwrapped (the caller
// decides whether one unreadable file aborts the pass); a write failure is
// tagged with errReindexWrite. skipVanished turns a missing file into a skip
// rather than an error, which the batch pass wants and a single named file does
// not.
func (p *Pipeline) reindexFile(ctx context.Context, absPath, uri, collection string, cfg chunk.Config, skipVanished bool) (result, error) {
	r, err := p.prepare(ctx, scanned{absPath: absPath, uri: uri}, Options{
		Collection:   collection,
		Chunking:     cfg,
		Force:        true,
		skipVanished: skipVanished,
	})
	if err != nil {
		return result{}, err
	}
	if r.skip {
		return r, nil
	}
	if err := p.write(ctx, r); err != nil {
		return result{}, fmt.Errorf("%w %q: %w", errReindexWrite, uri, err)
	}

	return r, nil
}

// ReindexOne re-parses and rewrites a single file, always forcing, and returns
// its document id and chunk count. Unlike IngestPath it never short-circuits on
// an unchanged content hash: it backs an editor's save, where "the index now
// reflects this file" must hold even when the saved bytes happen to match what
// was already indexed. A file prepare declines (binary, oversize, unparseable)
// is an error here rather than a silent skip, because the caller asked for this
// one file by name.
func (p *Pipeline) ReindexOne(ctx context.Context, absPath, uri, collection string, cfg chunk.Config) (docID string, chunks int, err error) {
	r, err := p.reindexFile(ctx, absPath, uri, collection, cfg, false)
	if err != nil {
		return "", 0, err
	}
	if r.skip {
		return "", 0, fmt.Errorf("ingest: reindex %q: not indexable (binary, oversize, or unparseable)", uri)
	}

	return r.doc.ID, len(r.chunks), nil
}

// ReindexOne performs a one-shot forced reindex of a single explicit file,
// mirroring File's wrapper shape over the pipeline. absPath is the file to read;
// uri is the stable, tree-root-relative identifier stored as documents.uri.
func ReindexOne(ctx context.Context, db *sql.DB, logger *slog.Logger, absPath, uri, collection string, cfg chunk.Config) (docID string, chunks int, err error) {
	return New(db, logger).ReindexOne(ctx, absPath, uri, collection, cfg)
}
