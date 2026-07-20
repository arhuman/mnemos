package search

import "github.com/arhuman/mnemos/internal/model"

// cliffDropRatio and cliffFloorRatio tune RelevanceCliff. A hit scoring below
// cliffDropRatio of its immediate predecessor is treated as falling off a cliff;
// a hit below cliffFloorRatio of the top hit is treated as filler regardless of
// the step from its predecessor. Both are relative so the heuristic is scale-free
// across queries whose bm25 magnitudes differ by orders of magnitude. Kept
// deliberately loose so only clear filler is trimmed, never a genuine long tail.
const (
	cliffDropRatio  = 0.4
	cliffFloorRatio = 0.2
)

// RelevanceCliff trims a ranked result slice at the first steep score drop-off so
// callers are not fed low-relevance filler just to reach the limit. results must
// be sorted best-first. The top hit is always kept; the scan then cuts before the
// first hit that is either far below the top score (the floor) or a steep
// fraction of its predecessor (the cliff). It only trims — it never reorders or
// grows the slice. When the top score is non-positive (e.g. a raw cosine ranking
// where the ratios are not meaningful) the slice is returned unchanged rather
// than guessing a cut.
func RelevanceCliff(results []model.Result) []model.Result {
	if len(results) <= 1 {
		return results
	}
	top := results[0].Score
	if top <= 0 {
		return results
	}
	for i := 1; i < len(results); i++ {
		prev := results[i-1].Score
		cur := results[i].Score
		if cur < cliffFloorRatio*top || (prev > 0 && cur < cliffDropRatio*prev) {
			return results[:i]
		}
	}

	return results
}
