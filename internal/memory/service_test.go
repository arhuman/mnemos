package memory_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/browse"
	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/testutil"
)

// testChunking is the deterministic chunk sizing the seed helpers ingest with.
var testChunking = chunk.Config{TargetTokens: 700, OverlapTokens: 80}

// fixture bundles a service over a freshly migrated store rooted at a temp dir,
// the underlying db (so tests can seed it directly), and the retriever the
// search/context verbs run through.
type fixture struct {
	svc       *memory.Service
	db        *sql.DB
	retriever search.Retriever
	treeRoot  string
}

// newFixture builds a service with the given write/delete gates. The store and
// tree root are per-test temporaries; the config is the built-in default with
// only the gate flags overridden.
func newFixture(t *testing.T, allowWrite, allowDelete bool) fixture {
	t.Helper()

	cfg, err := config.Load("", func(string) bool { return false })
	require.NoError(t, err)
	cfg.MCP.AllowWrite = allowWrite
	cfg.MCP.AllowDelete = allowDelete

	db := testutil.NewDB(t)
	logger := testutil.DiscardLogger()
	treeRoot := t.TempDir()
	// The capture dir is relative; in real use the cwd is the tree root, so chdir
	// there to keep auto-named remember notes under treeRoot.
	testutil.Chdir(t, treeRoot)

	return fixture{
		svc:       memory.New(db, cfg, treeRoot, nil, logger),
		db:        db,
		retriever: search.NewEngine(db, logger),
		treeRoot:  treeRoot,
	}
}

// seed writes rel under the tree root and ingests it under collection, returning
// the tree-relative uri.
func (f fixture) seed(t *testing.T, rel, content, collection string) string {
	t.Helper()
	abs := testutil.WriteFile(t, f.treeRoot, rel, content)
	uri := filepath.ToSlash(rel)
	_, _, err := ingest.File(context.Background(), f.db, testutil.DiscardLogger(), abs, uri, collection, testChunking)
	require.NoError(t, err)

	return uri
}

// newFixtureSeeded builds a read-only fixture seeded with a single SCIM doc.
func newFixtureSeeded(t *testing.T) fixture {
	t.Helper()
	f := newFixture(t, false, false)
	f.seed(t, "docs/scim.md", "# Provisioning\n\nSCIM provisioning syncs users automatically.\n", "default")

	return f
}

func TestSearchAndContext(t *testing.T) {
	f := newFixtureSeeded(t)

	results, err := f.svc.Search(context.Background(), f.retriever, search.Query{Text: "provisioning"})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Equal(t, "docs/scim.md", results[0].URI)

	blocks, err := f.svc.Context(context.Background(), f.retriever, search.Query{Text: "provisioning"})
	require.NoError(t, err)
	require.NotEmpty(t, blocks)
	require.Contains(t, blocks[0].Source, "docs/scim.md:")
	require.NotEmpty(t, blocks[0].Content)
}

func TestReadDocumentChunkAndReadOne(t *testing.T) {
	f := newFixtureSeeded(t)
	ctx := context.Background()

	doc, err := f.svc.ReadDocument(ctx, "docs/scim.md")
	require.NoError(t, err)
	require.Contains(t, doc.Content, "Provisioning")

	results, err := f.svc.Search(ctx, f.retriever, search.Query{Text: "provisioning"})
	require.NoError(t, err)
	got, err := f.svc.ReadChunk(ctx, results[0].ID)
	require.NoError(t, err)
	require.NotNil(t, got.Citation)
	require.Equal(t, "docs/scim.md", got.Citation.URI)

	_, err = f.svc.ReadOne(ctx, "docs/scim.md", "some-chunk")
	require.ErrorIs(t, err, memory.ErrAmbiguousRead)
	_, err = f.svc.ReadOne(ctx, "", "")
	require.ErrorIs(t, err, memory.ErrEmptyRead)
	_, err = f.svc.ReadDocument(ctx, "missing.md")
	require.Error(t, err)
	_, err = f.svc.ReadChunk(ctx, "no-such-chunk")
	require.Error(t, err)
}

func TestList(t *testing.T) {
	f := newFixtureSeeded(t)
	ctx := context.Background()

	entries, err := f.svc.List(ctx, browse.Options{})
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	_, err = f.svc.List(ctx, browse.Options{IndexedOnly: true, UnindexedOnly: true})
	require.ErrorContains(t, err, "mutually exclusive")
}

func TestRememberGating(t *testing.T) {
	ctx := context.Background()

	// Write disabled: refused.
	f := newFixture(t, false, false)
	_, err := f.svc.Remember(ctx, memory.RememberInput{Type: "idea", Text: "hello"})
	require.ErrorContains(t, err, "allow_write")

	// Write enabled: note is written, indexed, and findable.
	fw := newFixture(t, true, false)
	res, err := fw.svc.Remember(ctx, memory.RememberInput{Type: "idea", Text: "the answer is 42", Collection: "default"})
	require.NoError(t, err)
	require.NotEmpty(t, res.URI)
	require.Positive(t, res.Chunks)
	require.FileExists(t, filepath.Join(fw.treeRoot, filepath.FromSlash(res.URI)))

	hits, err := fw.svc.Search(ctx, fw.retriever, search.Query{Text: "answer"})
	require.NoError(t, err)
	require.NotEmpty(t, hits)
}

func TestRememberValidation(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, true, false)

	_, err := f.svc.Remember(ctx, memory.RememberInput{Type: "  ", Text: "x"})
	require.ErrorContains(t, err, "type must be")
	_, err = f.svc.Remember(ctx, memory.RememberInput{Type: "idea", Text: "   "})
	require.ErrorContains(t, err, "text must be")
	_, err = f.svc.Remember(ctx, memory.RememberInput{Type: "idea", Text: strings.Repeat("a", 65*1024)})
	require.ErrorContains(t, err, "too large")

	// A secret in the body is rejected before any write.
	_, err = f.svc.Remember(ctx, memory.RememberInput{Type: "idea", Text: "aws key AKIAIOSFODNN7EXAMPLE here"})
	require.ErrorContains(t, err, "secrets")
}

func TestOkfyUngated(t *testing.T) {
	ctx := context.Background()
	// Okfy is not gated by allow_write in the service (the local operator runs it
	// freely); the MCP adapter applies the gate at its boundary.
	f := newFixture(t, false, false)
	testutil.WriteFile(t, f.treeRoot, "note.txt", "Plain note body.\n")

	res, err := f.svc.Okfy(ctx, memory.OkfyInput{Source: "note.txt"})
	require.NoError(t, err)
	require.Equal(t, "note.md", res.URI)
	require.Positive(t, res.Chunks)
	require.FileExists(t, filepath.Join(f.treeRoot, "note.md"))
}

func TestForget(t *testing.T) {
	ctx := context.Background()

	// Delete disabled: refused.
	f := newFixture(t, false, false)
	_, err := f.svc.Forget(ctx, "x.md")
	require.ErrorContains(t, err, "allow_delete")

	// Delete enabled: removes file and de-indexes; idempotent on missing.
	fd := newFixture(t, false, true)
	abs := filepath.Join(fd.treeRoot, "note.md")
	fd.seed(t, "note.md", "# Note\n\nbody.\n", "c")

	res, err := fd.svc.Forget(ctx, "note.md")
	require.NoError(t, err)
	require.True(t, res.Deleted)
	require.NoFileExists(t, abs)

	missing, err := fd.svc.Forget(ctx, "gone.md")
	require.NoError(t, err)
	require.False(t, missing.Deleted)

	// Traversal is rejected by the confinement guard.
	_, err = fd.svc.Forget(ctx, "../escape.md")
	require.Error(t, err)
}

func TestMove(t *testing.T) {
	ctx := context.Background()

	// Delete disabled: refused.
	f := newFixture(t, false, false)
	_, err := f.svc.Move(ctx, "a.md", "b.md")
	require.ErrorContains(t, err, "allow_delete")

	fd := newFixture(t, false, true)
	fd.seed(t, "a.md", "# A\n\nbody.\n", "c")

	res, err := fd.svc.Move(ctx, "a.md", "kept.md")
	require.NoError(t, err)
	require.Equal(t, "a.md", res.From)
	require.Equal(t, "kept.md", res.To)
	require.FileExists(t, filepath.Join(fd.treeRoot, "kept.md"))

	// The two resolve errors are tagged so a surface can tell them apart.
	_, err = fd.svc.Move(ctx, "../escape.md", "ok.md")
	require.ErrorContains(t, err, "source:")
	_, err = fd.svc.Move(ctx, "kept.md", "../escape.md")
	require.ErrorContains(t, err, "destination:")
}
