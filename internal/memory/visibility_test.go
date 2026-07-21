package memory_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/browse"
	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/storage"
	"github.com/arhuman/mnemos/internal/testutil"
)

// newVisibilityFixture builds a read-only service whose config hides the given
// collections, seeded with one "provisioning" doc per collection so a query
// matches across all of them. The returned db lets read-path tests fetch a hidden
// document's chunk id directly (search never reveals it).
func newVisibilityFixture(t *testing.T, deny []string, collections ...string) (*memory.Service, *sql.DB, search.Retriever) {
	t.Helper()

	cfg, err := config.Load("", func(string) bool { return false })
	require.NoError(t, err)
	cfg.Security.Visibility.Deny = deny

	db := testutil.NewDB(t)
	logger := testutil.DiscardLogger()
	treeRoot := t.TempDir()
	testutil.Chdir(t, treeRoot)

	for _, c := range collections {
		rel := c + "/doc.md"
		abs := testutil.WriteFile(t, treeRoot, rel, "# Provisioning\n\nSCIM provisioning notes for "+c+".\n")
		_, _, err := ingest.File(context.Background(), db, logger, abs, filepath.ToSlash(rel), c, testChunking)
		require.NoError(t, err)
	}

	return memory.New(db, cfg, treeRoot, nil, logger), db, search.NewEngine(db, logger)
}

func TestSearchHidesDeniedCollection(t *testing.T) {
	svc, _, ret := newVisibilityFixture(t, []string{"perso"}, "mnemos", "perso")
	ctx := context.Background()

	results, err := svc.Search(ctx, ret, search.Query{Text: "provisioning"})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	for _, r := range results {
		require.NotEqual(t, "perso", r.Collection, "denied collection must never surface in search")
	}
	// The visible collection is still returned, with its provenance.
	require.Equal(t, "mnemos", results[0].Collection)
}

// TestSearchDenyIgnoresCallerCollection proves the boundary is server-side: even
// if the caller explicitly asks for the hidden collection, it stays hidden.
func TestSearchDenyIgnoresCallerCollection(t *testing.T) {
	svc, _, ret := newVisibilityFixture(t, []string{"perso"}, "mnemos", "perso")

	results, err := svc.Search(context.Background(), ret, search.Query{Text: "provisioning", Collection: "perso"})
	require.NoError(t, err)
	require.Empty(t, results, "a hidden collection cannot be reached even when named explicitly")
}

func TestContextHidesDeniedCollection(t *testing.T) {
	svc, _, ret := newVisibilityFixture(t, []string{"perso"}, "mnemos", "perso")

	blocks, err := svc.ContextWithOptions(context.Background(), ret, search.Query{Text: "provisioning"}, memory.ContextOptions{GroupByDocument: true})
	require.NoError(t, err)
	require.NotEmpty(t, blocks)
	for _, b := range blocks {
		require.NotEqual(t, "perso", b.Collection, "denied collection must never surface in context")
	}
}

func TestListHidesDeniedCollection(t *testing.T) {
	svc, _, _ := newVisibilityFixture(t, []string{"perso"}, "mnemos", "perso")

	entries, err := svc.List(context.Background(), browse.Options{})
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, e := range entries {
		require.NotEqual(t, "perso", e.Collection, "denied collection must never surface in list")
	}
}

func TestEmptyDenyShowsEverything(t *testing.T) {
	svc, _, ret := newVisibilityFixture(t, nil, "mnemos", "perso")

	results, err := svc.Search(context.Background(), ret, search.Query{Text: "provisioning", Limit: 20})
	require.NoError(t, err)
	got := make(map[string]bool)
	for _, r := range results {
		got[r.Collection] = true
	}
	require.True(t, got["mnemos"] && got["perso"], "empty deny list hides nothing")
}

// TestReadHidesDeniedCollectionByURI proves mnemos.read cannot reach a hidden
// collection by naming its document uri, and that the refusal is indistinguishable
// from a genuinely absent uri (no existence oracle). The visible collection still
// reads.
func TestReadHidesDeniedCollectionByURI(t *testing.T) {
	svc, _, _ := newVisibilityFixture(t, []string{"perso"}, "mnemos", "perso")
	ctx := context.Background()

	// The hidden uri must return the exact "unknown uri" form the not-found
	// branch produces for that same identifier — an absent "perso/absent.md"
	// yields the identical shape, so read cannot be used as an existence oracle.
	_, absentErr := svc.ReadDocument(ctx, "perso/absent.md")
	require.EqualError(t, absentErr, `unknown uri "perso/absent.md"`)
	_, hiddenErr := svc.ReadDocument(ctx, "perso/doc.md")
	require.EqualError(t, hiddenErr, `unknown uri "perso/doc.md"`,
		"a hidden collection must be unreadable by uri and shaped exactly like an absent one")

	// A scoped read of the hidden uri must also report unknown-uri, not a
	// section/line scope-mismatch (which would leak that the document exists).
	_, scopedErr := svc.ReadDocumentOpts(ctx, "perso/doc.md", memory.ReadOptions{Section: "provisioning"})
	require.EqualError(t, scopedErr, `unknown uri "perso/doc.md"`,
		"a matching section on a hidden uri must not distinguish it from an absent one")

	got, err := svc.ReadDocument(ctx, "mnemos/doc.md")
	require.NoError(t, err, "a visible collection must remain readable")
	require.Equal(t, "mnemos", got.Collection)
}

// TestReadHidesDeniedCollectionByChunkID proves the chunk read path enforces the
// same boundary: a chunk in a hidden collection returns the same not-found as an
// unknown chunk_id, while a visible chunk still reads.
func TestReadHidesDeniedCollectionByChunkID(t *testing.T) {
	svc, db, _ := newVisibilityFixture(t, []string{"perso"}, "mnemos", "perso")
	ctx := context.Background()

	hiddenChunks, err := storage.GetChunksByDocURI(ctx, db, "perso/doc.md")
	require.NoError(t, err)
	require.NotEmpty(t, hiddenChunks)

	// A real chunk_id in a hidden collection must yield the same "unknown
	// chunk_id" form the not-found branch produces for that id, not a distinct
	// error that would confirm the chunk exists.
	_, hiddenErr := svc.ReadChunk(ctx, hiddenChunks[0].ID)
	require.EqualError(t, hiddenErr, fmt.Sprintf("unknown chunk_id %q", hiddenChunks[0].ID),
		"a chunk in a hidden collection must be indistinguishable from an unknown chunk_id")

	visibleChunks, err := storage.GetChunksByDocURI(ctx, db, "mnemos/doc.md")
	require.NoError(t, err)
	require.NotEmpty(t, visibleChunks)
	got, err := svc.ReadChunk(ctx, visibleChunks[0].ID)
	require.NoError(t, err, "a chunk in a visible collection must remain readable")
	require.Equal(t, "mnemos", got.Collection)
}
