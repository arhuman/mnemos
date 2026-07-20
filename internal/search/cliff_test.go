package search

import (
	"testing"

	"github.com/arhuman/mnemos/internal/model"
)

// scores builds a ranked result slice from bare scores, best-first.
func scores(vals ...float64) []model.Result {
	out := make([]model.Result, len(vals))
	for i, v := range vals {
		out[i] = model.Result{Score: v}
	}

	return out
}

func TestRelevanceCliff(t *testing.T) {
	cases := []struct {
		name string
		in   []model.Result
		want int // expected kept length
	}{
		{"empty", nil, 0},
		{"single", scores(5), 1},
		{"gentle slope keeps all", scores(10, 9, 8.5, 8), 4},
		{"steep drop cuts at cliff", scores(10, 9, 2, 1.9), 2},
		{"steep drop from top cuts immediately", scores(10, 3, 1.5), 1},
		{"filler below floor cut", scores(10, 4.5, 1.9), 2},
		{"top hit always kept even if next is zero", scores(10, 0), 1},
		{"non-positive top left untouched", scores(-1, -2, -50), 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RelevanceCliff(tc.in)
			if len(got) != tc.want {
				t.Fatalf("kept %d results, want %d (scores %v)", len(got), tc.want, tc.in)
			}
		})
	}
}

// TestRelevanceCliffNeverGrowsOrReorders guards the "only trims" contract.
func TestRelevanceCliffNeverGrowsOrReorders(t *testing.T) {
	in := scores(10, 9.5, 9, 1)
	got := RelevanceCliff(in)
	if len(got) > len(in) {
		t.Fatalf("cliff grew the slice: %d > %d", len(got), len(in))
	}
	for i := range got {
		if got[i].Score != in[i].Score {
			t.Fatalf("cliff reordered at %d: got %v want %v", i, got[i].Score, in[i].Score)
		}
	}
}
