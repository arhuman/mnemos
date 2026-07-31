package search_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/storage"
	"github.com/arhuman/mnemos/internal/testutil"
)

// putDoc inserts a document with a single searchable chunk.
func putDoc(t *testing.T, db *sql.DB, id, uri, collection, content string) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, storage.UpsertDocument(ctx, tx, model.Document{
		ID: id, URI: uri, Collection: collection, ContentHash: "h" + id, Title: id, IndexedAt: "t",
	}))
	require.NoError(t, storage.ReplaceChunks(ctx, tx, id, []model.Chunk{{
		ID: id + "-c0", DocumentID: id, Ordinal: 0, Content: content, StartLine: 1, EndLine: 1,
	}}))
	require.NoError(t, tx.Commit())
}

func putLinks(t *testing.T, db *sql.DB, srcID string, dstURIs ...string) {
	t.Helper()
	links := make([]model.Link, 0, len(dstURIs))
	for _, d := range dstURIs {
		links = append(links, model.Link{SrcDoc: srcID, DstDoc: d})
	}
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, storage.ReplaceLinks(context.Background(), tx, srcID, links))
	require.NoError(t, tx.Commit())
}

func resultURIs(rs []model.Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.URI
	}

	return out
}

// TestGraphRetrieverInjectsNeighborWhenUnderfilled: a query hits only doc A, and
// A links to B (which shares no keyword with the query). Expansion fills the empty
// slots with B, so B is recovered without changing A's position.
func TestGraphRetrieverInjectsNeighborWhenUnderfilled(t *testing.T) {
	db := testutil.NewDB(t)
	putDoc(t, db, "a", "a.md", "demo", "alpha unique-term-a")
	putDoc(t, db, "b", "b.md", "demo", "beta unrelated-words")
	putLinks(t, db, "a", "b.md") // a -> b

	gr := search.NewGraphRetriever(search.NewEngine(db, nil), db, 3, 0.5, nil)
	res, err := gr.Search(context.Background(), search.Query{Text: "alpha", Limit: 3})
	require.NoError(t, err)

	uris := resultURIs(res)
	require.Equal(t, "a.md", uris[0], "base hit stays first")
	require.Contains(t, uris, "b.md", "neighbor recovered into an empty slot")
}

// TestGraphRetrieverNeverEvictsBase: when the base already fills the limit,
// expansion appends nothing, so the result is exactly the base list.
func TestGraphRetrieverNeverEvictsBase(t *testing.T) {
	db := testutil.NewDB(t)
	putDoc(t, db, "a", "a.md", "demo", "alpha term")
	putDoc(t, db, "b", "b.md", "demo", "beta term")
	putLinks(t, db, "a", "b.md")

	engine := search.NewEngine(db, nil)
	gr := search.NewGraphRetriever(engine, db, 3, 0.5, nil)

	// "term" matches both docs; limit 1 means the base already fills the limit.
	base, err := engine.Search(context.Background(), search.Query{Text: "term", Limit: 1})
	require.NoError(t, err)
	require.Len(t, base, 1)

	got, err := gr.Search(context.Background(), search.Query{Text: "term", Limit: 1})
	require.NoError(t, err)
	require.Equal(t, resultURIs(base), resultURIs(got), "full base is returned unchanged")
}

// TestGraphRetrieverRespectsVisibilityDeny: a hidden-collection neighbor is not
// injected even though the link exists.
func TestGraphRetrieverRespectsVisibilityDeny(t *testing.T) {
	db := testutil.NewDB(t)
	putDoc(t, db, "a", "a.md", "demo", "alpha term-a")
	putDoc(t, db, "b", "b.md", "secret", "beta term-b")
	putLinks(t, db, "a", "b.md")

	gr := search.NewGraphRetriever(search.NewEngine(db, nil), db, 3, 0.5, nil)
	res, err := gr.Search(context.Background(), search.Query{
		Text:               "alpha",
		Limit:              3,
		ExcludeCollections: []string{"secret"},
	})
	require.NoError(t, err)
	require.NotContains(t, resultURIs(res), "b.md", "hidden neighbor must not be injected")
}

// TestGraphRetrieverSkipsDanglingNeighbor: an outbound link to an un-ingested doc
// has no chunk to cite, so it is never injected.
func TestGraphRetrieverSkipsDanglingNeighbor(t *testing.T) {
	db := testutil.NewDB(t)
	putDoc(t, db, "a", "a.md", "demo", "alpha term-a")
	putLinks(t, db, "a", "ghost.md") // dangling

	gr := search.NewGraphRetriever(search.NewEngine(db, nil), db, 3, 0.5, nil)
	res, err := gr.Search(context.Background(), search.Query{Text: "alpha", Limit: 3})
	require.NoError(t, err)
	require.Equal(t, []string{"a.md"}, resultURIs(res))
}
