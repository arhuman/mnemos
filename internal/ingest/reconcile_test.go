package ingest_test

import (
	"context"
	"database/sql"
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

var reconcileChunking = chunk.Config{TargetTokens: 64, OverlapTokens: 0}

// seedTree ingests a two-file tree and returns its root and pipeline.
func seedTree(t *testing.T) (string, *ingest.Pipeline, *sql.DB) {
	t.Helper()
	root := t.TempDir()
	write(t, root, "a.md", "# A\n\nBody\n")
	write(t, root, "sub/b.md", "# B\n\nBody\n")

	db := newDB(t)
	p := ingest.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := p.Run(context.Background(), ingest.Options{
		Root:       root,
		Collection: "c",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   reconcileChunking,
	})
	require.NoError(t, err)

	return root, p, db
}

func TestIngestRunEvictsDeletedFile(t *testing.T) {
	root, p, db := seedTree(t)
	require.NoError(t, os.Remove(filepath.Join(root, "sub", "b.md")))

	sum, err := p.Run(context.Background(), ingest.Options{
		Root:       root,
		Collection: "c",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   reconcileChunking,
		Reconcile:  true,
	})
	require.NoError(t, err)
	require.Equal(t, 1, sum.FilesRemoved)

	doc, err := storage.GetDocumentByURI(context.Background(), db, "sub/b.md")
	require.NoError(t, err)
	require.Nil(t, doc, "a deleted file must stop being citable")

	doc, err = storage.GetDocumentByURI(context.Background(), db, "a.md")
	require.NoError(t, err)
	require.NotNil(t, doc)
}

func TestIngestRunWithoutReconcileKeepsDeletedFile(t *testing.T) {
	root, p, db := seedTree(t)
	require.NoError(t, os.Remove(filepath.Join(root, "sub", "b.md")))

	sum, err := p.Run(context.Background(), ingest.Options{
		Root:       root,
		Collection: "c",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   reconcileChunking,
	})
	require.NoError(t, err)
	require.Equal(t, 0, sum.FilesRemoved, "reconcile is opt-in")

	doc, err := storage.GetDocumentByURI(context.Background(), db, "sub/b.md")
	require.NoError(t, err)
	require.NotNil(t, doc)
}

// TestReconcileScopesToSubtree proves a subtree reindex never prunes documents
// outside it: a sibling directory's files are untouched even though they are
// absent from the scanned subtree.
func TestReconcileScopesToSubtree(t *testing.T) {
	root, p, db := seedTree(t)
	require.NoError(t, os.Remove(filepath.Join(root, "a.md")))

	sum, err := p.Run(context.Background(), ingest.Options{
		Root:       filepath.Join(root, "sub"),
		URIBase:    root,
		Collection: "c",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   reconcileChunking,
		Reconcile:  true,
	})
	require.NoError(t, err)
	require.Equal(t, 0, sum.FilesRemoved, "a.md sits outside the scanned subtree")

	doc, err := storage.GetDocumentByURI(context.Background(), db, "a.md")
	require.NoError(t, err)
	require.NotNil(t, doc, "reconcile must not prune outside its scope")
}

// TestReconcilePrefixIsSegmentAware proves a prefix matches at path boundaries
// only, so scanning "sub" never reconciles a sibling named "sub-archive".
func TestReconcilePrefixIsSegmentAware(t *testing.T) {
	root := t.TempDir()
	write(t, root, "sub/b.md", "# B\n\nBody\n")
	write(t, root, "sub-archive/c.md", "# C\n\nBody\n")

	db := newDB(t)
	p := ingest.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	_, err := p.Run(ctx, ingest.Options{
		Root:       root,
		Collection: "c",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   reconcileChunking,
	})
	require.NoError(t, err)

	require.NoError(t, os.Remove(filepath.Join(root, "sub-archive", "c.md")))

	sum, err := p.Run(ctx, ingest.Options{
		Root:       filepath.Join(root, "sub"),
		URIBase:    root,
		Collection: "c",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   reconcileChunking,
		Reconcile:  true,
	})
	require.NoError(t, err)
	require.Equal(t, 0, sum.FilesRemoved)

	doc, err := storage.GetDocumentByURI(ctx, db, "sub-archive/c.md")
	require.NoError(t, err)
	require.NotNil(t, doc, "\"sub\" must not match \"sub-archive\"")
}

// TestReconcileEvictsDanglingSymlink proves the link case from #42: the link file
// still exists on disk, but its target is gone, so the document is evicted.
func TestReconcileEvictsDanglingSymlink(t *testing.T) {
	ext := t.TempDir()
	target := filepath.Join(ext, "note.md")
	require.NoError(t, os.WriteFile(target, []byte("# Note\n\nBody\n"), 0o600))

	root := t.TempDir()
	link := filepath.Join(root, "note.md")
	require.NoError(t, os.Symlink(target, link))

	db := newDB(t)
	p := ingest.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	_, err := p.Run(ctx, ingest.Options{
		Root:       root,
		Collection: "c",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   reconcileChunking,
	})
	require.NoError(t, err)

	doc, err := storage.GetDocumentByURI(ctx, db, "note.md")
	require.NoError(t, err)
	require.NotNil(t, doc, "the linked file indexes")

	require.NoError(t, os.Remove(target))

	sum, err := p.Run(ctx, ingest.Options{
		Root:       root,
		Collection: "c",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   reconcileChunking,
		Reconcile:  true,
	})
	require.NoError(t, err, "a dangling link must not abort the run")
	require.Equal(t, 1, sum.FilesRemoved)

	doc, err = storage.GetDocumentByURI(ctx, db, "note.md")
	require.NoError(t, err)
	require.Nil(t, doc, "a link whose target is deleted must stop being citable")
}

func TestReconcileMissingRootEvictsNothing(t *testing.T) {
	root, p, db := seedTree(t)

	_, err := p.Run(context.Background(), ingest.Options{
		Root:       filepath.Join(root, "gone"),
		URIBase:    root,
		Collection: "c",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   reconcileChunking,
		Reconcile:  true,
	})
	require.Error(t, err)

	for _, uri := range []string{"a.md", "sub/b.md"} {
		doc, err := storage.GetDocumentByURI(context.Background(), db, uri)
		require.NoError(t, err)
		require.NotNil(t, doc, "a missing root must not evict %q", uri)
	}
}
