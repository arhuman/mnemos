package ingest_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/storage"
)

// TestReindexOneRewritesMutatedContent proves the editor save path picks up a
// changed file: the stored title follows the file.
func TestReindexOneRewritesMutatedContent(t *testing.T) {
	src := t.TempDir()
	write(t, src, "a.md", "---\ntitle: First\n---\n\n## Goal\n\nBody\n")
	abs := filepath.Join(src, "a.md")

	db := newDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	docID, chunks, err := ingest.ReindexOne(ctx, db, logger, abs, "a.md", "c", reindexChunking)
	require.NoError(t, err)
	require.NotEmpty(t, docID)
	require.Positive(t, chunks)

	write(t, src, "a.md", "---\ntitle: Second\n---\n\n## Goal\n\nBody\n")

	_, _, err = ingest.ReindexOne(ctx, db, logger, abs, "a.md", "c", reindexChunking)
	require.NoError(t, err)

	doc, err := storage.GetDocumentByURI(ctx, db, "a.md")
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Equal(t, "Second", doc.Title)
}

// TestReindexOneForcesUnchangedContent is the behavioral difference from
// IngestPath: byte-identical content is re-parsed and rewritten rather than
// skipped, so a save always leaves the index reflecting the file.
func TestReindexOneForcesUnchangedContent(t *testing.T) {
	src := t.TempDir()
	write(t, src, "a.md", "---\ntitle: Real Title\n---\n\n## Goal\n\nBody\n")
	abs := filepath.Join(src, "a.md")

	db := newDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	_, chunks, err := ingest.ReindexOne(ctx, db, logger, abs, "a.md", "c", reindexChunking)
	require.NoError(t, err)
	require.Positive(t, chunks)

	// IngestPath would return zero chunks here (hash unchanged); ReindexOne forces.
	_, skipped, err := ingest.New(db, logger).IngestPath(ctx, abs, "a.md", "c", reindexChunking)
	require.NoError(t, err)
	require.Zero(t, skipped)

	_, again, err := ingest.ReindexOne(ctx, db, logger, abs, "a.md", "c", reindexChunking)
	require.NoError(t, err)
	require.Equal(t, chunks, again, "a forced reindex rewrites the same chunks rather than skipping")
}

// TestReindexOneRejectsUnindexableFile proves a file prepare declines surfaces as
// a descriptive error instead of a silent no-op, because the caller named this
// one file.
func TestReindexOneRejectsUnindexableFile(t *testing.T) {
	src := t.TempDir()
	write(t, src, "big.md", strings.Repeat("x", 4096))
	abs := filepath.Join(src, "big.md")

	db := newDB(t)
	p := ingest.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)), ingest.WithMaxFileBytes(16))

	_, _, err := p.ReindexOne(context.Background(), abs, "big.md", "c", reindexChunking)
	require.ErrorContains(t, err, "not indexable")
}

// TestReindexOneMissingFileErrors proves a vanished file is an error for the
// single-file path (the batch pass counts it as missing and continues instead).
func TestReindexOneMissingFileErrors(t *testing.T) {
	db := newDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	_, _, err := ingest.ReindexOne(context.Background(), db, logger,
		filepath.Join(t.TempDir(), "gone.md"), "gone.md", "c", reindexChunking)
	require.Error(t, err)
	require.True(t, os.IsNotExist(err) || strings.Contains(err.Error(), "stat"))
}
