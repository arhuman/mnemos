package search

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/model"
)

func res(id string) model.Result { return model.Result{ID: id} }

func ids(rs []model.Result) []string {
	out := make([]string, len(rs))
	for i, r := range rs {
		out[i] = r.ID
	}

	return out
}

func TestReciprocalRankFusion(t *testing.T) {
	t.Run("a result ranked by both lists wins", func(t *testing.T) {
		lexical := []model.Result{res("c1"), res("c2"), res("c3")}
		vector := []model.Result{res("c2"), res("c3"), res("c4")}

		fused := reciprocalRankFusion([][]model.Result{lexical, vector}, 60)

		// c2 (ranks 2 and 1) and c3 (ranks 3 and 2) appear in both lists and
		// outrank the singletons c1 (rank 1) and c4 (rank 3).
		require.Equal(t, []string{"c2", "c3", "c1", "c4"}, ids(fused))
	})

	t.Run("scores follow the RRF formula", func(t *testing.T) {
		lexical := []model.Result{res("a"), res("b")}
		vector := []model.Result{res("b"), res("a")}

		fused := reciprocalRankFusion([][]model.Result{lexical, vector}, 60)

		// Both a and b sit at ranks 1 and 2 across the two lists, so both score
		// 1/61 + 1/62. The tie breaks on ID ascending.
		want := 1.0/61.0 + 1.0/62.0
		require.Equal(t, []string{"a", "b"}, ids(fused))
		require.InDelta(t, want, fused[0].Score, 1e-12)
		require.InDelta(t, want, fused[1].Score, 1e-12)
	})

	t.Run("single non-empty list reproduces its order (graceful degrade)", func(t *testing.T) {
		lexical := []model.Result{res("x"), res("y"), res("z")}

		fused := reciprocalRankFusion([][]model.Result{lexical, nil}, 60)

		require.Equal(t, []string{"x", "y", "z"}, ids(fused))
	})

	t.Run("metadata comes from the first list a result appears in", func(t *testing.T) {
		lexical := []model.Result{{ID: "c1", Snippet: "lexical snippet", URI: "doc.md"}}
		vector := []model.Result{{ID: "c1", Snippet: "vector snippet", URI: "other.md"}}

		fused := reciprocalRankFusion([][]model.Result{lexical, vector}, 60)

		require.Len(t, fused, 1)
		require.Equal(t, "lexical snippet", fused[0].Snippet)
		require.Equal(t, "doc.md", fused[0].URI)
		// Its fused score sums both lists' rank-1 contributions.
		require.InDelta(t, 2.0/61.0, fused[0].Score, 1e-12)
	})

	t.Run("empty input yields empty output", func(t *testing.T) {
		require.Empty(t, reciprocalRankFusion(nil, 60))
		require.Empty(t, reciprocalRankFusion([][]model.Result{nil, nil}, 60))
	})
}
