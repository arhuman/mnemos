package ingest_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/ingest"
)

// TestMatchPredicate exercises the exported Match predicate the watcher shares
// with the batch scanner: included iff it matches an include glob and no
// exclude/security glob. Pure function — no filesystem, no mocks.
func TestMatchPredicate(t *testing.T) {
	include := []string{"**/*.md", "**/*.go"}
	exclude := []string{"vendor/**", "**/_*.md"}
	security := []string{"**/*.key", "**/secrets/**"}

	cases := []struct {
		name string
		rel  string
		want bool
	}{
		{"markdown included", "docs/a.md", true},
		{"go included", "internal/x.go", true},
		{"native separators normalized", "docs\\sub\\b.md", true},
		{"extension not in include", "notes/a.txt", false},
		{"excluded by indexing glob", "vendor/lib/a.go", false},
		{"excluded by underscore rule", "docs/_draft.md", false},
		{"excluded by security key", "config/server.key", false},
		{"excluded by security secrets dir", "app/secrets/token.md", false},
		{"empty include set matches nothing", "docs/a.md", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ingest.Match(tc.rel, include, exclude, security))
		})
	}

	t.Run("nil include matches nothing", func(t *testing.T) {
		require.False(t, ingest.Match("docs/a.md", nil, nil, nil))
	})
}
