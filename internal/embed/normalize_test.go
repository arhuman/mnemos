package embed

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestL2Normalize(t *testing.T) {
	t.Run("scales to unit length", func(t *testing.T) {
		v := []float32{3, 4}
		L2Normalize(v)
		require.InDelta(t, 0.6, v[0], 1e-6)
		require.InDelta(t, 0.8, v[1], 1e-6)
		require.InDelta(t, 1.0, norm(v), 1e-6)
	})

	t.Run("zero vector is left unchanged", func(t *testing.T) {
		v := []float32{0, 0, 0}
		L2Normalize(v)
		require.Equal(t, []float32{0, 0, 0}, v)
	})

	t.Run("already-normalized vector stays unit length", func(t *testing.T) {
		v := []float32{0, 1, 0}
		L2Normalize(v)
		require.InDelta(t, 1.0, norm(v), 1e-6)
	})
}

func norm(v []float32) float64 {
	var s float64
	for _, x := range v {
		s += float64(x) * float64(x)
	}

	return math.Sqrt(s)
}
