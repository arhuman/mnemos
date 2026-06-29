package parse

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHasFrontmatter(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "unix delimiter", content: "---\ntype: note\n---\n\nbody\n", want: true},
		{name: "windows delimiter", content: "---\r\ntype: note\r\n---\r\n", want: true},
		{name: "no frontmatter", content: "# Heading\n\nbody\n", want: false},
		{name: "empty", content: "", want: false},
		{name: "dashes mid-line", content: "intro\n---\nbody\n", want: false},
		{name: "dashes without newline", content: "---", want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, HasFrontmatter([]byte(tc.content)))
		})
	}
}
