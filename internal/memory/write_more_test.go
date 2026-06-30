package memory_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/browse"
	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/testutil"
)

// newFixtureWith builds a fixture from the default config after applying mutate,
// so a test can flip capture/gate settings the plain newFixture does not expose.
func newFixtureWith(t *testing.T, mutate func(*config.Config)) fixture {
	t.Helper()

	cfg, err := config.Load("", func(string) bool { return false })
	require.NoError(t, err)
	mutate(cfg)

	db := testutil.NewDB(t)
	logger := testutil.DiscardLogger()
	treeRoot := t.TempDir()
	testutil.Chdir(t, treeRoot)

	return fixture{
		svc:       memory.New(db, cfg, treeRoot, nil, logger),
		db:        db,
		retriever: search.NewEngine(db, logger),
		treeRoot:  treeRoot,
	}
}

// TestNewDefaults covers New's nil-cfg, nil-scanner, nil-logger and
// non-positive default-limit fallbacks in one construction.
func TestNewDefaults(t *testing.T) {
	require.NotNil(t, memory.New(nil, nil, "", nil, nil))
}

// TestRememberToPath covers writeNoteFile's caller-chosen-path branch: a fresh
// write creates the file under a nested directory, a second write to the same
// path is an update, a non-.md path is rejected, and traversal is confined.
func TestRememberToPath(t *testing.T) {
	f := newFixture(t, true, false)
	ctx := context.Background()

	res, err := f.svc.Remember(ctx, memory.RememberInput{Type: "idea", Text: "path note body", Path: "notes/captured.md"})
	require.NoError(t, err)
	require.Equal(t, "notes/captured.md", res.URI)
	require.Positive(t, res.Chunks)
	require.FileExists(t, filepath.Join(f.treeRoot, "notes", "captured.md"))

	// Re-writing the same path is an update (exercises the LogUpdate branch).
	res2, err := f.svc.Remember(ctx, memory.RememberInput{Type: "idea", Text: "updated note body", Path: "notes/captured.md"})
	require.NoError(t, err)
	require.Equal(t, "notes/captured.md", res2.URI)

	// A non-.md target is rejected.
	_, err = f.svc.Remember(ctx, memory.RememberInput{Type: "idea", Text: "x", Path: "notes/bad.txt"})
	require.ErrorContains(t, err, ".md")

	// Traversal outside the tree is rejected by the confinement guard.
	_, err = f.svc.Remember(ctx, memory.RememberInput{Type: "idea", Text: "x", Path: "../escape.md"})
	require.ErrorContains(t, err, "remember path")
}

// TestRememberDeferred covers the defer_to_watcher write-only branch: the file
// is written but not indexed, so the result is marked deferred with no chunks.
func TestRememberDeferred(t *testing.T) {
	f := newFixtureWith(t, func(c *config.Config) {
		c.MCP.AllowWrite = true
		c.Capture.DeferToWatcher = true
	})

	res, err := f.svc.Remember(context.Background(), memory.RememberInput{Type: "idea", Text: "deferred body"})
	require.NoError(t, err)
	require.True(t, res.Deferred)
	require.Zero(t, res.Chunks)
	require.Empty(t, res.DocumentID)
	require.NotEmpty(t, res.URI)
	require.FileExists(t, filepath.Join(f.treeRoot, filepath.FromSlash(res.URI)))
}

// TestReadOneDispatch covers ReadOne's successful uri and chunk_id arms (the
// invalid-selector arms are covered in service_test.go).
func TestReadOneDispatch(t *testing.T) {
	f := newFixtureSeeded(t)
	ctx := context.Background()

	doc, err := f.svc.ReadOne(ctx, "docs/scim.md", "")
	require.NoError(t, err)
	require.Contains(t, doc.Content, "Provisioning")

	results, err := f.svc.Search(ctx, f.retriever, search.Query{Text: "provisioning"})
	require.NoError(t, err)
	require.NotEmpty(t, results)

	chunkRes, err := f.svc.ReadOne(ctx, "", results[0].ID)
	require.NoError(t, err)
	require.NotNil(t, chunkRes.Citation)
	require.Equal(t, "docs/scim.md", chunkRes.Citation.URI)
}

// TestWriteVerbsHonorContextCancellation covers the ctx.Err() guard at the head
// of each write verb (checked before the gate, so a cancelled context fails
// fast regardless of allow_write/allow_delete).
func TestWriteVerbsHonorContextCancellation(t *testing.T) {
	f := newFixture(t, true, true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := f.svc.Remember(ctx, memory.RememberInput{Type: "idea", Text: "x"})
	require.ErrorIs(t, err, context.Canceled)
	_, err = f.svc.Okfy(ctx, memory.OkfyInput{Source: "x.txt"})
	require.ErrorIs(t, err, context.Canceled)
	_, err = f.svc.Forget(ctx, "x.md")
	require.ErrorIs(t, err, context.Canceled)
	_, err = f.svc.Move(ctx, "a.md", "b.md")
	require.ErrorIs(t, err, context.Canceled)
}

// TestListNormalizesEmpty covers the nil-to-empty-slice normalization on an
// empty tree (List must never hand a surface a nil slice).
func TestListNormalizesEmpty(t *testing.T) {
	f := newFixture(t, false, false)

	entries, err := f.svc.List(context.Background(), browse.Options{})
	require.NoError(t, err)
	require.NotNil(t, entries)
	require.Empty(t, entries)
}
