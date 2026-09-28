package ingest_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/storage"
)

var reindexChunking = chunk.Config{TargetTokens: 700, OverlapTokens: 80}

// TestForceReingestsUnchanged proves Options.Force bypasses the content-hash skip:
// a second run of an unchanged corpus re-ingests every file instead of skipping.
func TestForceReingestsUnchanged(t *testing.T) {
	src := t.TempDir()
	write(t, src, "a.md", "# Title\n\nBody\n")
	write(t, src, "b.md", "# Other\n\nMore\n")

	db := newDB(t)
	p := ingest.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	opts := ingest.Options{
		Root:       src,
		Collection: "c",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   reindexChunking,
	}

	sum, err := p.Run(context.Background(), opts)
	require.NoError(t, err)
	require.Equal(t, 2, sum.FilesIngested)

	// Unchanged second run without force skips everything.
	sum2, err := p.Run(context.Background(), opts)
	require.NoError(t, err)
	require.Equal(t, 0, sum2.FilesIngested)
	require.Equal(t, 2, sum2.FilesSkipped)

	// With force, the same unchanged files are re-ingested.
	opts.Force = true
	sum3, err := p.Run(context.Background(), opts)
	require.NoError(t, err)
	require.Equal(t, 2, sum3.FilesIngested)
	require.Equal(t, 0, sum3.FilesSkipped)
}

// TestReindexContentRefreshesStoredFields proves a content reindex re-parses every
// indexed document from disk and rewrites its stored fields — even when the file
// itself is unchanged — by restoring a title that was corrupted in the index.
func TestReindexContentRefreshesStoredFields(t *testing.T) {
	src := t.TempDir()
	write(t, src, "a.md", "---\ntitle: Real Title\n---\n\n## Goal\n\nBody\n")

	db := newDB(t)
	p := ingest.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	_, err := p.Run(ctx, ingest.Options{
		Root:       src,
		Collection: "c",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   reindexChunking,
	})
	require.NoError(t, err)

	// Corrupt the stored title to simulate a document indexed before a parser fix.
	_, err = db.ExecContext(ctx, `UPDATE documents SET title = 'STALE' WHERE uri = 'a.md'`)
	require.NoError(t, err)

	sum, err := p.ReindexContent(ctx, src, reindexChunking)
	require.NoError(t, err)
	require.Equal(t, 1, sum.Documents)
	require.Equal(t, 1, sum.Reindexed)
	require.Zero(t, sum.Missing)

	doc, err := storage.GetDocumentByURI(ctx, db, "a.md")
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Equal(t, "Real Title", doc.Title, "reindex must rewrite the stored title from the file")
	require.Equal(t, "c", doc.Collection, "reindex preserves the stored collection")
}

// TestReindexContentEvictsDeletedFile proves a document whose backing file is
// gone is evicted rather than left citable (#42). The reconcile pass runs before
// the document listing, so the deleted row is neither counted nor re-read.
func TestReindexContentEvictsDeletedFile(t *testing.T) {
	src := t.TempDir()
	write(t, src, "a.md", "# A\n\nBody\n")
	write(t, src, "b.md", "# B\n\nBody\n")

	db := newDB(t)
	p := ingest.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	_, err := p.Run(ctx, ingest.Options{
		Root:       src,
		Collection: "c",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   reindexChunking,
	})
	require.NoError(t, err)

	require.NoError(t, os.Remove(filepath.Join(src, "b.md")))

	sum, err := p.ReindexContent(ctx, src, reindexChunking)
	require.NoError(t, err)
	require.Equal(t, 1, sum.Removed, "the deleted file's document is evicted")
	require.Equal(t, 1, sum.Documents, "eviction precedes the listing, so only the live document is considered")
	require.Equal(t, 1, sum.Reindexed)
	require.Equal(t, 0, sum.Missing, "missing now means unreadable-but-present, not deleted")

	// The deleted file's row is gone, so it can no longer be cited.
	doc, err := storage.GetDocumentByURI(ctx, db, "b.md")
	require.NoError(t, err)
	require.Nil(t, doc)

	// The surviving document is untouched.
	doc, err = storage.GetDocumentByURI(ctx, db, "a.md")
	require.NoError(t, err)
	require.NotNil(t, doc)
}

// TestReindexContentUnreadableRootEvictsNothing proves an unreadable kb root fails
// the pass without evicting anything: absence below a root that cannot be read
// proves nothing, and an unmounted volume must not empty the index.
func TestReindexContentUnreadableRootEvictsNothing(t *testing.T) {
	src := t.TempDir()
	write(t, src, "a.md", "# A\n\nBody\n")

	db := newDB(t)
	p := ingest.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	_, err := p.Run(ctx, ingest.Options{
		Root:       src,
		Collection: "c",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   reindexChunking,
	})
	require.NoError(t, err)

	_, err = p.ReindexContent(ctx, filepath.Join(src, "does-not-exist"), reindexChunking)
	require.ErrorIs(t, err, ingest.ErrReconcileRootMissing)

	doc, err := storage.GetDocumentByURI(ctx, db, "a.md")
	require.NoError(t, err)
	require.NotNil(t, doc, "a missing root must not evict anything")
}
