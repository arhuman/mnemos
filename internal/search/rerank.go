package search

import (
	"cmp"
	"slices"
	"strings"

	"github.com/arhuman/mnemos/internal/model"
)

// rrfK is the Reciprocal Rank Fusion smoothing constant. The standard value
// (~60) dampens the contribution of top ranks just enough that a result must
// rank well in at least one list to surface, while a result ranked highly by
// both lists still wins. It is intentionally not tunable per query.
const rrfK = 60

// reciprocalRankFusion fuses several ranked result lists into one. Each input
// list must already be ordered best-first. A result's fused score is the sum,
// over the lists it appears in, of 1/(k + rank) where rank is its 1-based
// position in that list. Results are merged by their chunk ID; the first
// occurrence (earliest list) supplies the returned metadata, so callers should
// pass the list with the richer snippet (lexical) first. The output is sorted by
// fused score descending, with ID as a deterministic tie-breaker, and each
// result's Score is set to its fused score.
//
// RRF is rank-based, not score-based: it needs no score normalization between
// bm25 (unbounded, negative-origin) and cosine (0..1), which is exactly why it
// is robust for fusing heterogeneous retrievers.
func reciprocalRankFusion(lists [][]model.Result, k int) []model.Result {
	type entry struct {
		res   model.Result
		score float64
	}
	merged := make(map[string]*entry)
	var order []string

	for _, list := range lists {
		for rank, r := range list {
			contrib := 1.0 / float64(k+rank+1)
			if e, ok := merged[r.ID]; ok {
				e.score += contrib

				continue
			}
			merged[r.ID] = &entry{res: r, score: contrib}
			order = append(order, r.ID)
		}
	}

	out := make([]model.Result, 0, len(order))
	for _, id := range order {
		e := merged[id]
		e.res.Score = e.score
		out = append(out, e.res)
	}

	slices.SortStableFunc(out, func(a, b model.Result) int {
		if a.Score != b.Score {
			return cmp.Compare(b.Score, a.Score)
		}

		return strings.Compare(a.ID, b.ID)
	})

	return out
}
