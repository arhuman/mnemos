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

// seedDoc inserts one document with its chunks, so Similar has several documents
// to rank against each other (seedCorpus seeds only the single shared document).
func seedDoc(t *testing.T, db *sql.DB, id, uri, title string, chunks []model.Chunk) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, storage.UpsertDocument(ctx, tx, model.Document{
		ID: id, URI: uri, Collection: "c", Title: title, ContentHash: "h" + id, IndexedAt: "t",
	}))
	require.NoError(t, storage.ReplaceChunks(ctx, tx, id, chunks))
	require.NoError(t, tx.Commit())
}

// seedSimilarCorpus builds three documents: the source (pointing along x), a
// near neighbour, and a far one.
func seedSimilarCorpus(t *testing.T, db *sql.DB) {
	t.Helper()
	seedDoc(t, db, "src", "src.md", "Source", []model.Chunk{
		{ID: "s0", DocumentID: "src", Ordinal: 0, Content: "alpha"},
		{ID: "s1", DocumentID: "src", Ordinal: 1, Content: "beta"},
	})
	seedDoc(t, db, "near", "near.md", "Near", []model.Chunk{
		{ID: "n0", DocumentID: "near", Ordinal: 0, Content: "gamma"},
	})
	seedDoc(t, db, "far", "far.md", "Far", []model.Chunk{
		{ID: "f0", DocumentID: "far", Ordinal: 0, Content: "delta"},
	})

	// The source pools to the x axis; near is close to it, far is orthogonal.
	seedVec(t, db, "s0", []float32{1, 0})
	seedVec(t, db, "s1", []float32{0.8, 0.6})
	seedVec(t, db, "n0", []float32{0.95, 0.3})
	seedVec(t, db, "f0", []float32{-1, 0})
}

// TestSimilarRanksOtherDocuments proves Similar pools the source document's own
// vectors, ranks the rest by proximity, and never returns the source itself.
func TestSimilarRanksOtherDocuments(t *testing.T) {
	db := testutil.NewDB(t)
	seedSimilarCorpus(t, db)

	got, err := search.Similar(context.Background(), db, "m", "src.md", 10)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "near.md", got[0].URI)
	require.Equal(t, "Near", got[0].Title)
	require.Equal(t, "c", got[0].Collection)
	require.Equal(t, "far.md", got[1].URI)
	require.Greater(t, got[0].Score, got[1].Score)

	for _, r := range got {
		require.NotEqual(t, "src.md", r.URI, "the source document must never rank against itself")
	}
}

// TestSimilarRespectsLimit proves the ranked list is truncated, keeping the best.
func TestSimilarRespectsLimit(t *testing.T) {
	db := testutil.NewDB(t)
	seedSimilarCorpus(t, db)

	got, err := search.Similar(context.Background(), db, "m", "src.md", 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "near.md", got[0].URI)
}

// TestSimilarScoresDocumentByItsBestChunk proves a document is ranked by its
// closest chunk, so extra distant chunks never dilute it.
func TestSimilarScoresDocumentByItsBestChunk(t *testing.T) {
	db := testutil.NewDB(t)
	seedDoc(t, db, "src", "src.md", "Source", []model.Chunk{{ID: "s0", DocumentID: "src", Ordinal: 0, Content: "a"}})
	seedDoc(t, db, "multi", "multi.md", "Multi", []model.Chunk{
		{ID: "m0", DocumentID: "multi", Ordinal: 0, Content: "b"},
		{ID: "m1", DocumentID: "multi", Ordinal: 1, Content: "c"},
	})
	seedVec(t, db, "s0", []float32{1, 0})
	seedVec(t, db, "m0", []float32{0, 1})
	seedVec(t, db, "m1", []float32{1, 0})

	got, err := search.Similar(context.Background(), db, "m", "src.md", 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.InDelta(t, 1.0, got[0].Score, 1e-6)
}

// TestSimilarWithoutVectorsIsEmptyNotAnError proves an un-embedded corpus (or an
// un-embedded source) degrades to "unavailable" rather than failing.
func TestSimilarWithoutVectorsIsEmptyNotAnError(t *testing.T) {
	db := testutil.NewDB(t)
	seedDoc(t, db, "src", "src.md", "Source", []model.Chunk{{ID: "s0", DocumentID: "src", Ordinal: 0, Content: "a"}})
	seedDoc(t, db, "other", "other.md", "Other", []model.Chunk{{ID: "o0", DocumentID: "other", Ordinal: 0, Content: "b"}})
	seedVec(t, db, "o0", []float32{1, 0})

	got, err := search.Similar(context.Background(), db, "m", "src.md", 10)
	require.NoError(t, err)
	require.Empty(t, got)
}

// TestSimilarUnknownURIErrors proves an unresolvable uri is a real error, unlike
// a resolvable one that simply has no vectors.
func TestSimilarUnknownURIErrors(t *testing.T) {
	db := testutil.NewDB(t)

	_, err := search.Similar(context.Background(), db, "m", "nope.md", 10)
	require.ErrorContains(t, err, "unknown uri")
}

// TestSimilarSkipsDimensionDrift proves a vector stored at another dimension is
// skipped instead of corrupting the ranking.
func TestSimilarSkipsDimensionDrift(t *testing.T) {
	db := testutil.NewDB(t)
	seedDoc(t, db, "src", "src.md", "Source", []model.Chunk{{ID: "s0", DocumentID: "src", Ordinal: 0, Content: "a"}})
	seedDoc(t, db, "drift", "drift.md", "Drift", []model.Chunk{{ID: "d0", DocumentID: "drift", Ordinal: 0, Content: "b"}})
	seedVec(t, db, "s0", []float32{1, 0})
	seedVec(t, db, "d0", []float32{1, 0, 0})

	got, err := search.Similar(context.Background(), db, "m", "src.md", 10)
	require.NoError(t, err)
	require.Empty(t, got)
}
