package memory_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/browse"
	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/testutil"
)

// newVisibilityFixture builds a read-only service whose config hides the given
// collections, seeded with one "provisioning" doc per collection so a query
// matches across all of them.
func newVisibilityFixture(t *testing.T, deny []string, collections ...string) (*memory.Service, search.Retriever) {
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

	return memory.New(db, cfg, treeRoot, nil, logger), search.NewEngine(db, logger)
}

func TestSearchHidesDeniedCollection(t *testing.T) {
	svc, ret := newVisibilityFixture(t, []string{"perso"}, "mnemos", "perso")
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
	svc, ret := newVisibilityFixture(t, []string{"perso"}, "mnemos", "perso")

	results, err := svc.Search(context.Background(), ret, search.Query{Text: "provisioning", Collection: "perso"})
	require.NoError(t, err)
	require.Empty(t, results, "a hidden collection cannot be reached even when named explicitly")
}

func TestContextHidesDeniedCollection(t *testing.T) {
	svc, ret := newVisibilityFixture(t, []string{"perso"}, "mnemos", "perso")

	blocks, err := svc.ContextWithOptions(context.Background(), ret, search.Query{Text: "provisioning"}, memory.ContextOptions{GroupByDocument: true})
	require.NoError(t, err)
	require.NotEmpty(t, blocks)
	for _, b := range blocks {
		require.NotEqual(t, "perso", b.Collection, "denied collection must never surface in context")
	}
}

func TestListHidesDeniedCollection(t *testing.T) {
	svc, _ := newVisibilityFixture(t, []string{"perso"}, "mnemos", "perso")

	entries, err := svc.List(context.Background(), browse.Options{})
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, e := range entries {
		require.NotEqual(t, "perso", e.Collection, "denied collection must never surface in list")
	}
}

func TestEmptyDenyShowsEverything(t *testing.T) {
	svc, ret := newVisibilityFixture(t, nil, "mnemos", "perso")

	results, err := svc.Search(context.Background(), ret, search.Query{Text: "provisioning", Limit: 20})
	require.NoError(t, err)
	got := make(map[string]bool)
	for _, r := range results {
		got[r.Collection] = true
	}
	require.True(t, got["mnemos"] && got["perso"], "empty deny list hides nothing")
}
