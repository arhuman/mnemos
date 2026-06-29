package model_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/model"
)

func TestLastHeading(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"single segment", "Intro", "Intro"},
		{"single segment trimmed", "  Intro  ", "Intro"},
		{"two segments", "A > B", "B"},
		{"three segments", "A > B > C", "C"},
		{"trailing spaces around last", "A > B >  C ", "C"},
		{"empty last segment", "A >", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, model.LastHeading(tc.in))
		})
	}
}
