package eval

import "github.com/arhuman/mnemos/internal/model"

// Metrics holds the doc-level retrieval scores over all evaluated pairs. All
// rates are in [0,1]. N is the number of example-query pairs evaluated.
type Metrics struct {
	N          int     `json:"n"`
	HitAt1     float64 `json:"hit_at_1"`
	RecallAtK  float64 `json:"recall_at_k"`
	MRRAtK     float64 `json:"mrr_at_k"`
	ExactChunk float64 `json:"exact_chunk"`
	K          int     `json:"k"`
}

// evalCase pairs the ground-truth pair with the results its query produced, so
// metrics can be computed in one pass.
type evalCase struct {
	pair    pair
	results []model.Result
}

// compute aggregates the per-query results into doc-level metrics. Credit is
// doc-level: any chunk of the expected document counts as a hit.
//
//   - Hit@1: fraction where the top-1 result's uri == expectedURI.
//   - Recall@K: fraction where expectedURI appears anywhere in the top-K.
//   - MRR@K: mean of 1/rank of the first result whose uri == expectedURI
//     (0 when absent).
//   - exact-chunk (secondary, stricter): fraction where the top-1 result is the
//     specific chunk the query was stripped from, approximated by host uri plus
//     a line range that contains the block's original start line.
func compute(cases []evalCase, k int) Metrics {
	m := Metrics{N: len(cases), K: k}
	if len(cases) == 0 {
		return m
	}

	var hits, recalls, exact int
	var rrSum float64
	for _, c := range cases {
		if topMatches(c.results, c.pair.expectedURI) {
			hits++
		}
		if rank := firstRank(c.results, c.pair.expectedURI); rank > 0 {
			recalls++
			rrSum += 1.0 / float64(rank)
		}
		if topIsExactChunk(c.results, c.pair) {
			exact++
		}
	}

	n := float64(len(cases))
	m.HitAt1 = float64(hits) / n
	m.RecallAtK = float64(recalls) / n
	m.MRRAtK = rrSum / n
	m.ExactChunk = float64(exact) / n

	return m
}

// topMatches reports whether the first result belongs to expectedURI.
func topMatches(results []model.Result, expectedURI string) bool {
	return len(results) > 0 && results[0].URI == expectedURI
}

// firstRank returns the 1-based rank of the first result whose uri matches
// expectedURI, or 0 when none does.
func firstRank(results []model.Result, expectedURI string) int {
	for i, r := range results {
		if r.URI == expectedURI {
			return i + 1
		}
	}

	return 0
}

// topIsExactChunk reports whether the top-1 result is the specific chunk the
// example query was stripped from. Exact-chunk identity is approximated as: the
// top result's uri is the host uri AND the chunk's 1-based line range contains
// the block's original start line. This is best-effort — stripping shifts later
// lines, so the match is anchored on the block's original start line, which sits
// at or before the chunk boundary in the common case.
func topIsExactChunk(results []model.Result, p pair) bool {
	if len(results) == 0 {
		return false
	}
	top := results[0]
	if top.URI != p.expectedURI {
		return false
	}

	return p.blockStartLine >= top.StartLine && p.blockStartLine <= top.EndLine
}
