package ingest

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/storage"
)

// ReindexContentSummary reports the outcome of a content reindex.
type ReindexContentSummary struct {
	// Documents is how many indexed documents were considered.
	Documents int
	// Reindexed is how many were re-parsed and rewritten.
	Reindexed int
	// Skipped is how many prepare declined (binary/oversize/unparseable on disk).
	Skipped int
	// Missing is how many had no readable backing file (deleted or moved); their
	// existing index rows are left untouched, not removed.
	Missing int
	// Chunks is the total chunks rewritten across reindexed documents.
	Chunks int
}

// ReindexContent re-parses and rewrites every already-indexed document from its
// on-disk file, bypassing the content-hash skip, so a parser or schema change
// (e.g. a corrected title derivation) propagates to the whole index without
// editing files. It walks the documents table rather than the filesystem, so each
// document keeps its stored collection and only indexed documents are touched.
//
// A document whose file is gone from disk is left in place with a warning (a
// content reindex refreshes, it does not reconcile deletions). Rewriting a
// document's chunks cascades away its stored embeddings (they key on chunk id);
// callers that use semantic search should run `reindex --embeddings` afterwards.
func (p *Pipeline) ReindexContent(ctx context.Context, kbRoot string, cfg chunk.Config) (ReindexContentSummary, error) {
	docs, err := storage.ListDocuments(ctx, p.db, storage.ListFilter{})
	if err != nil {
		return ReindexContentSummary{}, fmt.Errorf("ingest: reindex list documents: %w", err)
	}

	sum := ReindexContentSummary{Documents: len(docs)}
	for _, d := range docs {
		if err := ctx.Err(); err != nil {
			return sum, err
		}
		abs := filepath.Join(kbRoot, filepath.FromSlash(d.URI))
		r, err := p.prepare(ctx, scanned{absPath: abs, uri: d.URI}, Options{
			Collection: d.Collection,
			Chunking:   cfg,
			Force:      true,
		})
		if err != nil {
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
		if err := p.write(ctx, r); err != nil {
			return sum, fmt.Errorf("ingest: reindex write %q: %w", d.URI, err)
		}
		sum.Reindexed++
		sum.Chunks += len(r.chunks)
	}

	return sum, nil
}
