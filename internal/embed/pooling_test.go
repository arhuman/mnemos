package embed

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMeanPool(t *testing.T) {
	tests := []struct {
		name   string
		flat   []float32
		mask   []int64
		batch  int
		seqLen int
		dim    int
		want   [][]float32
	}{
		{
			name:   "all tokens unmasked averages every position",
			flat:   []float32{1, 2, 3, 4}, // token0=[1,2] token1=[3,4]
			mask:   []int64{1, 1},
			batch:  1,
			seqLen: 2,
			dim:    2,
			want:   [][]float32{{2, 3}},
		},
		{
			name:   "masked token is excluded from the mean",
			flat:   []float32{1, 2, 3, 4},
			mask:   []int64{1, 0},
			batch:  1,
			seqLen: 2,
			dim:    2,
			want:   [][]float32{{1, 2}},
		},
		{
			name:   "no unmasked tokens yields a zero vector",
			flat:   []float32{5, 6, 7, 8},
			mask:   []int64{0, 0},
			batch:  1,
			seqLen: 2,
			dim:    2,
			want:   [][]float32{{0, 0}},
		},
		{
			name:   "two rows pool independently",
			flat:   []float32{1, 1, 3, 3 /* row0 */, 2, 4, 0, 0 /* row1 */},
			mask:   []int64{1, 1, 1, 0},
			batch:  2,
			seqLen: 2,
			dim:    2,
			want:   [][]float32{{2, 2}, {2, 4}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MeanPool(tt.flat, tt.mask, tt.batch, tt.seqLen, tt.dim)
			require.Len(t, got, tt.batch)
			for i := range tt.want {
				require.InDeltaSlice(t, toF64(tt.want[i]), toF64(got[i]), 1e-6)
			}
		})
	}
}

func toF64(v []float32) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = float64(x)
	}

	return out
}
