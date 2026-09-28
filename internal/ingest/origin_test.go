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

var originChunking = chunk.Config{TargetTokens: 64, OverlapTokens: 0}

func originRules() ingest.Rules { return ingest.Rules{Include: []string{"**/*.md"}} }

// externalTree builds a kb root and a separate external tree with two files.
func externalTree(t *testing.T) (kb, ext string, db *sql.DB, p *ingest.Pipeline) {
	t.Helper()
	kb = t.TempDir()
	ext = t.TempDir()
	write(t, ext, "a.md", "# A\n\nalpha\n")
	write(t, ext, "sub/b.md", "# B\n\nbeta\n")
	db = newDB(t)
	p = ingest.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))

	return kb, ext, db, p
}

func TestRegisterAndReindexOrigin(t *testing.T) {
	kb, ext, db, p := externalTree(t)
	ctx := context.Background()

	o, err := ingest.RegisterOrigin(ctx, db, kb, "spec", ext, "pias")
	require.NoError(t, err)
	require.Equal(t, "spec", o.Prefix)

	sum, err := p.ReindexOrigin(ctx, o, originRules(), originChunking)
	require.NoError(t, err)
	require.Equal(t, 2, sum.Indexed)

	// URIs carry the namespace, and nothing was copied into the kb.
	for _, uri := range []string{"spec/a.md", "spec/sub/b.md"} {
		doc, docErr := storage.GetDocumentByURI(ctx, db, uri)
		require.NoError(t, docErr)
		require.NotNil(t, doc, "expected %q", uri)
		require.Equal(t, "pias", doc.Collection)
	}
	entries, err := os.ReadDir(kb)
	require.NoError(t, err)
	require.Empty(t, entries, "an origin is indexed in place, never copied")
}

// TestTwoOriginsShareRelativePaths is the capability #39 asked for: two
// independent trees with the same relative path in one store.
func TestTwoOriginsShareRelativePaths(t *testing.T) {
	kb := t.TempDir()
	extA, extB := t.TempDir(), t.TempDir()
	write(t, extA, "note.md", "# A\n\nalpha content\n")
	write(t, extB, "note.md", "# B\n\nbeta content\n")

	db := newDB(t)
	p := ingest.New(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()

	for _, tc := range []struct{ prefix, path string }{{"spec", extA}, {"code", extB}} {
		o, err := ingest.RegisterOrigin(ctx, db, kb, tc.prefix, tc.path, "c")
		require.NoError(t, err)
		_, err = p.ReindexOrigin(ctx, o, originRules(), originChunking)
		require.NoError(t, err)
	}

	for _, uri := range []string{"spec/note.md", "code/note.md"} {
		doc, err := storage.GetDocumentByURI(ctx, db, uri)
		require.NoError(t, err)
		require.NotNil(t, doc, "identical relative paths must coexist under distinct prefixes: %q", uri)
	}
}

func TestOriginReindexPicksUpNewAndDeletedFiles(t *testing.T) {
	kb, ext, db, p := externalTree(t)
	ctx := context.Background()
	o, err := ingest.RegisterOrigin(ctx, db, kb, "spec", ext, "c")
	require.NoError(t, err)
	_, err = p.ReindexOrigin(ctx, o, originRules(), originChunking)
	require.NoError(t, err)

	write(t, ext, "c.md", "# C\n\ngamma\n")
	require.NoError(t, os.Remove(filepath.Join(ext, "a.md")))

	sum, err := p.ReindexOrigin(ctx, o, originRules(), originChunking)
	require.NoError(t, err)
	require.Equal(t, 1, sum.Indexed, "the new file is discovered")
	require.Equal(t, 1, sum.Removed, "the deleted file is evicted")

	doc, err := storage.GetDocumentByURI(ctx, db, "spec/c.md")
	require.NoError(t, err)
	require.NotNil(t, doc)

	doc, err = storage.GetDocumentByURI(ctx, db, "spec/a.md")
	require.NoError(t, err)
	require.Nil(t, doc)
}

// TestOriginReindexMissingRootEvictsNothing is the unmounted-volume case: an
// absent root must never be read as "every file was deleted".
func TestOriginReindexMissingRootEvictsNothing(t *testing.T) {
	kb, ext, db, p := externalTree(t)
	ctx := context.Background()
	o, err := ingest.RegisterOrigin(ctx, db, kb, "spec", ext, "c")
	require.NoError(t, err)
	_, err = p.ReindexOrigin(ctx, o, originRules(), originChunking)
	require.NoError(t, err)

	require.NoError(t, os.RemoveAll(ext))

	_, err = p.ReindexOrigin(ctx, o, originRules(), originChunking)
	require.ErrorIs(t, err, ingest.ErrReconcileRootMissing)

	doc, err := storage.GetDocumentByURI(ctx, db, "spec/a.md")
	require.NoError(t, err)
	require.NotNil(t, doc, "a missing root must not evict the namespace")
}

// TestKBReindexIgnoresOriginDocuments proves a kb-anchored pass never judges an
// origin's documents: their files are not under the kb, so an unguarded pass
// would evict every one of them.
func TestKBReindexIgnoresOriginDocuments(t *testing.T) {
	kb, ext, db, p := externalTree(t)
	ctx := context.Background()
	write(t, kb, "own.md", "# Own\n\nkb content\n")

	_, err := p.Run(ctx, ingest.Options{
		Root: kb, Collection: "c", Rules: originRules(), Chunking: originChunking,
	})
	require.NoError(t, err)

	o, err := ingest.RegisterOrigin(ctx, db, kb, "spec", ext, "c")
	require.NoError(t, err)
	_, err = p.ReindexOrigin(ctx, o, originRules(), originChunking)
	require.NoError(t, err)

	sum, err := p.ReindexContent(ctx, kb, originChunking)
	require.NoError(t, err)
	require.Equal(t, 0, sum.Removed, "a kb reindex must not evict origin documents")
	require.Equal(t, 1, sum.Documents, "only the kb's own document is considered")

	doc, err := storage.GetDocumentByURI(ctx, db, "spec/a.md")
	require.NoError(t, err)
	require.NotNil(t, doc)
}

func TestRegisterOriginRejections(t *testing.T) {
	kb, ext, db, _ := externalTree(t)
	ctx := context.Background()

	t.Run("multi-segment prefix", func(t *testing.T) {
		_, err := ingest.RegisterOrigin(ctx, db, kb, "a/b", ext, "c")
		require.ErrorIs(t, err, ingest.ErrOriginPrefixInvalid)
	})

	t.Run("path is a file", func(t *testing.T) {
		_, err := ingest.RegisterOrigin(ctx, db, kb, "f", filepath.Join(ext, "a.md"), "c")
		require.ErrorIs(t, err, ingest.ErrOriginPathNotDir)
	})

	t.Run("path inside the kb", func(t *testing.T) {
		inside := filepath.Join(kb, "sub")
		require.NoError(t, os.MkdirAll(inside, 0o750))
		_, err := ingest.RegisterOrigin(ctx, db, kb, "ins", inside, "c")
		require.ErrorIs(t, err, ingest.ErrOriginInsideKB)
	})

	t.Run("prefix collides with kb content", func(t *testing.T) {
		require.NoError(t, os.MkdirAll(filepath.Join(kb, "taken"), 0o750))
		_, err := ingest.RegisterOrigin(ctx, db, kb, "taken", ext, "c")
		require.ErrorIs(t, err, ingest.ErrOriginPrefixTaken)
	})
}

func TestRemoveOriginEvictsNamespaceNotFiles(t *testing.T) {
	kb, ext, db, p := externalTree(t)
	ctx := context.Background()
	write(t, kb, "own.md", "# Own\n\nkb content\n")
	_, err := p.Run(ctx, ingest.Options{
		Root: kb, Collection: "c", Rules: originRules(), Chunking: originChunking,
	})
	require.NoError(t, err)

	o, err := ingest.RegisterOrigin(ctx, db, kb, "spec", ext, "c")
	require.NoError(t, err)
	_, err = p.ReindexOrigin(ctx, o, originRules(), originChunking)
	require.NoError(t, err)

	removed, err := ingest.RemoveOrigin(ctx, db, "spec")
	require.NoError(t, err)
	require.Equal(t, 2, removed)

	doc, err := storage.GetDocumentByURI(ctx, db, "spec/a.md")
	require.NoError(t, err)
	require.Nil(t, doc, "the namespace stops being citable")

	doc, err = storage.GetDocumentByURI(ctx, db, "own.md")
	require.NoError(t, err)
	require.NotNil(t, doc, "kb content is untouched")

	require.FileExists(t, filepath.Join(ext, "a.md"), "the origin's files are never deleted")

	_, err = storage.GetOrigin(ctx, db, "spec")
	require.ErrorIs(t, err, storage.ErrOriginNotFound)
}
