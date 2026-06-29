//go:build !embed

package search_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/embed"
	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/testutil"
)

// These tests pin the default-build (no embed tag) graceful-degradation
// contract: with the no-op embedder, vector search yields nothing, hybrid falls
// back to lexical, and reindex surfaces ErrNotSupported. They are tagged !embed
// because in an embed build embed.New constructs the real (model-loading)
// embedder instead of the no-op.

func TestVectorRetrieverNoopDegrades(t *testing.T) {
	db := testutil.NewDB(t)
	seedCorpus(t, db, []model.Chunk{{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "x"}})

	noop, err := embed.New("")
	require.NoError(t, err)
	vr := search.NewVectorRetriever(db, noop, nil)

	results, err := vr.Search(context.Background(), search.Query{Text: "x", Limit: 5})
	require.NoError(t, err, "no-op embedder must not error the vector search")
	require.Empty(t, results, "no-op embedder yields no vector hits")
}

func TestHybridDegradesToLexical(t *testing.T) {
	db := testutil.NewDB(t)
	seedCorpus(t, db, []model.Chunk{
		{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "alpha gamma"},
		{ID: "c1", DocumentID: "d", Ordinal: 1, Content: "beta"},
	})

	noop, err := embed.New("")
	require.NoError(t, err)
	engine := search.NewEngine(db, nil)
	vector := search.NewVectorRetriever(db, noop, nil)
	hybrid := search.NewHybridRetriever(engine, vector, nil)

	results, err := hybrid.Search(context.Background(), search.Query{Text: "alpha", Limit: 5})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "c0", results[0].ID, "hybrid must return the lexical hit when vectors are absent")
}

func TestReindexNoopErrors(t *testing.T) {
	db := testutil.NewDB(t)
	seedCorpus(t, db, []model.Chunk{{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "x"}})

	noop, err := embed.New("")
	require.NoError(t, err)
	_, err = search.Reindex(context.Background(), db, noop, nil)
	require.True(t, errors.Is(err, embed.ErrNotSupported))
}
