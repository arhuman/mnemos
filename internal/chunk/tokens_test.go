package chunk

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWordEstimatorCount(t *testing.T) {
	tc := WordEstimator{}
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"empty", "", 0},
		{"whitespace only", "   \n\t ", 0},
		{"single word", "hello", 2},              // ceil(1*1.3)=2
		{"two words", "hello world", 3},          // ceil(2*1.3)=3
		{"ten words", "a b c d e f g h i j", 13}, // ceil(10*1.3)=13
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			require.Equal(t, c.want, tc.Count(c.in))
		})
	}
}
