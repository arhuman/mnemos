package storage_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/storage"
	"github.com/arhuman/mnemos/internal/testutil"
)

// seedChunks inserts one document with the given chunks so embeddings (which
// have a FK to chunks) can reference real rows.
func seedChunks(t *testing.T, db *sql.DB, chunks []model.Chunk) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, storage.UpsertDocument(context.Background(), tx, model.Document{
		ID: "d", URI: "a.md", Collection: "c", ContentHash: "h", IndexedAt: "t",
	}))
	require.NoError(t, storage.ReplaceChunks(context.Background(), tx, "d", chunks))
	require.NoError(t, tx.Commit())
}

func upsertVec(t *testing.T, db *sql.DB, chunkID string, vec []float32) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, storage.UpsertEmbedding(context.Background(), tx, chunkID, "m", len(vec), storage.EncodeVector(vec)))
	require.NoError(t, tx.Commit())
}

func TestEncodeDecodeVectorRoundTrip(t *testing.T) {
	cases := [][]float32{
		{},
		{0},
		{1, -1, 0.5, -0.25},
		{3.14159, -2.71828, 0, 100000.5},
	}
	for _, vec := range cases {
		b := storage.EncodeVector(vec)
		require.Len(t, b, len(vec)*4)
		got, err := storage.DecodeVector(b)
		require.NoError(t, err)
		if len(vec) == 0 {
			require.Empty(t, got)

			continue
		}
		require.Equal(t, vec, got)
	}
}

func TestDecodeVectorBadLength(t *testing.T) {
	_, err := storage.DecodeVector([]byte{1, 2, 3}) // not a multiple of 4
	require.Error(t, err)
}

func TestUpsertGetEmbedding(t *testing.T) {
	db := testutil.NewDB(t)
	seedChunks(t, db, []model.Chunk{{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "x"}})

	upsertVec(t, db, "c0", []float32{1, 2, 3})
	gotModel, dim, vec, found, err := storage.GetEmbedding(context.Background(), db, "c0")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "m", gotModel)
	require.Equal(t, 3, dim)
	require.Equal(t, []float32{1, 2, 3}, vec)

	// Upsert again replaces the vector, not a second row.
	upsertVec(t, db, "c0", []float32{9, 8, 7})
	_, _, vec, _, err = storage.GetEmbedding(context.Background(), db, "c0")
	require.NoError(t, err)
	require.Equal(t, []float32{9, 8, 7}, vec)

	n, err := storage.CountEmbeddings(context.Background(), db, "m")
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

func TestGetEmbeddingMissing(t *testing.T) {
	db := testutil.NewDB(t)
	_, _, _, found, err := storage.GetEmbedding(context.Background(), db, "nope")
	require.NoError(t, err)
	require.False(t, found)
}

func TestDeleteEmbedding(t *testing.T) {
	db := testutil.NewDB(t)
	seedChunks(t, db, []model.Chunk{{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "x"}})
	upsertVec(t, db, "c0", []float32{1, 2, 3})

	require.NoError(t, storage.DeleteEmbedding(context.Background(), db, "c0"))
	_, _, _, found, err := storage.GetEmbedding(context.Background(), db, "c0")
	require.NoError(t, err)
	require.False(t, found)

	// Deleting a missing embedding is a no-op.
	require.NoError(t, storage.DeleteEmbedding(context.Background(), db, "c0"))
}

func TestEmbeddingCascadesWithChunk(t *testing.T) {
	db := testutil.NewDB(t)
	seedChunks(t, db, []model.Chunk{
		{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "x"},
		{ID: "c1", DocumentID: "d", Ordinal: 1, Content: "y"},
	})
	upsertVec(t, db, "c0", []float32{1, 0})
	upsertVec(t, db, "c1", []float32{0, 1})

	// Replacing the document's chunks deletes the old chunk rows; ON DELETE
	// CASCADE must evict their embeddings so no stale vectors linger.
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, storage.ReplaceChunks(context.Background(), tx, "d", []model.Chunk{
		{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "x-new"},
	}))
	require.NoError(t, tx.Commit())

	_, _, _, found, err := storage.GetEmbedding(context.Background(), db, "c1")
	require.NoError(t, err)
	require.False(t, found, "embedding for the removed chunk must be cascaded away")
}

func TestAllChunkRefs(t *testing.T) {
	db := testutil.NewDB(t)
	seedChunks(t, db, []model.Chunk{
		{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "alpha"},
		{ID: "c1", DocumentID: "d", Ordinal: 1, Content: "beta"},
	})

	refs, err := storage.AllChunkRefs(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, []storage.ChunkRef{
		{ID: "c0", Content: "alpha"},
		{ID: "c1", Content: "beta"},
	}, refs)
}
