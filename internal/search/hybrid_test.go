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

// stubEmbedder returns a fixed query vector, letting the vector scan be tested
// without an ONNX model.
type stubEmbedder struct{ vec []float32 }

func (s stubEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = s.vec
	}

	return out, nil
}
func (s stubEmbedder) Dim() int    { return len(s.vec) }
func (stubEmbedder) Model() string { return "m" }

func seedCorpus(t *testing.T, db *sql.DB, chunks []model.Chunk) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, storage.UpsertDocument(context.Background(), tx, model.Document{
		ID: "d", URI: "a.md", Collection: "c", ContentHash: "h", IndexedAt: "t",
	}))
	require.NoError(t, storage.ReplaceChunks(context.Background(), tx, "d", chunks))
	require.NoError(t, tx.Commit())
}

func seedVec(t *testing.T, db *sql.DB, chunkID string, vec []float32) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, storage.UpsertEmbedding(context.Background(), tx, chunkID, "m", len(vec), storage.EncodeVector(vec)))
	require.NoError(t, tx.Commit())
}

func TestVectorRetrieverRanksByCosine(t *testing.T) {
	db := testutil.NewDB(t)
	seedCorpus(t, db, []model.Chunk{
		{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "alpha"},
		{ID: "c1", DocumentID: "d", Ordinal: 1, Content: "beta"},
	})
	seedVec(t, db, "c0", []float32{1, 0})
	seedVec(t, db, "c1", []float32{0, 1})

	vr := search.NewVectorRetriever(db, stubEmbedder{vec: []float32{1, 0}}, nil)
	results, err := vr.Search(context.Background(), search.Query{Text: "anything", Limit: 5})
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, "c0", results[0].ID, "the aligned vector must rank first")
	require.InDelta(t, 1.0, results[0].Score, 1e-6)
	require.Equal(t, "c1", results[1].ID)
	require.Equal(t, "alpha", results[0].Snippet)
}

func TestVectorRetrieverRespectsLimit(t *testing.T) {
	db := testutil.NewDB(t)
	seedCorpus(t, db, []model.Chunk{
		{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "a"},
		{ID: "c1", DocumentID: "d", Ordinal: 1, Content: "b"},
	})
	seedVec(t, db, "c0", []float32{1, 0})
	seedVec(t, db, "c1", []float32{0.9, 0.1})

	vr := search.NewVectorRetriever(db, stubEmbedder{vec: []float32{1, 0}}, nil)
	results, err := vr.Search(context.Background(), search.Query{Text: "x", Limit: 1})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "c0", results[0].ID)
}

func TestHybridFusesBothSources(t *testing.T) {
	db := testutil.NewDB(t)
	seedCorpus(t, db, []model.Chunk{
		{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "alpha"},
		{ID: "c1", DocumentID: "d", Ordinal: 1, Content: "beta"},
	})
	// c1 has no lexical match for "alpha" but is the nearest vector, so fusion
	// must surface it alongside the lexical hit c0.
	seedVec(t, db, "c0", []float32{0, 1})
	seedVec(t, db, "c1", []float32{1, 0})

	engine := search.NewEngine(db, nil)
	vector := search.NewVectorRetriever(db, stubEmbedder{vec: []float32{1, 0}}, nil)
	hybrid := search.NewHybridRetriever(engine, vector, nil)

	results, err := hybrid.Search(context.Background(), search.Query{Text: "alpha", Limit: 5})
	require.NoError(t, err)
	gotIDs := make(map[string]bool)
	for _, r := range results {
		gotIDs[r.ID] = true
	}
	require.True(t, gotIDs["c0"], "lexical hit must survive fusion")
	require.True(t, gotIDs["c1"], "vector-only hit must survive fusion")
}

func TestReindexEmptyCorpus(t *testing.T) {
	db := testutil.NewDB(t)
	n, err := search.Reindex(context.Background(), db, stubEmbedder{vec: []float32{1, 0}}, nil)
	require.NoError(t, err)
	require.Zero(t, n)
}

func TestReindexWritesVectors(t *testing.T) {
	db := testutil.NewDB(t)
	seedCorpus(t, db, []model.Chunk{
		{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "a"},
		{ID: "c1", DocumentID: "d", Ordinal: 1, Content: "b"},
	})

	n, err := search.Reindex(context.Background(), db, stubEmbedder{vec: []float32{1, 0}}, nil)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	count, err := storage.CountEmbeddings(context.Background(), db, "m")
	require.NoError(t, err)
	require.Equal(t, 2, count)
}
