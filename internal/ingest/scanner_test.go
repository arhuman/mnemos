package ingest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeFile creates a file (and parent dirs) under root with empty content.
func writeFile(t *testing.T, root, rel string) {
	t.Helper()
	p := filepath.Join(root, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
}

func TestScanIncludeExcludeSecurity(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "docs/a.md")
	writeFile(t, root, "docs/b.txt")
	writeFile(t, root, "node_modules/pkg/c.md")
	writeFile(t, root, "secrets/token.md")
	writeFile(t, root, "image.png")

	rules := scanRules{
		include:         []string{"**/*.md", "**/*.txt"},
		exclude:         []string{"node_modules/**"},
		securityExclude: []string{"**/secrets/**"},
	}

	got, err := scan(root, rules)
	require.NoError(t, err)

	uris := make([]string, 0, len(got))
	for _, s := range got {
		uris = append(uris, s.uri)
		require.True(t, filepath.IsAbs(s.absPath), "abs path expected")
	}
	require.ElementsMatch(t, []string{"docs/a.md", "docs/b.txt"}, uris)
}

func TestScanSingleFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "only.md")
	file := filepath.Join(root, "only.md")

	got, err := scan(file, scanRules{include: []string{"**/*.md"}})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "only.md", got[0].uri)
}

func TestScanSingleFileExcluded(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "only.png")
	file := filepath.Join(root, "only.png")

	got, err := scan(file, scanRules{include: []string{"**/*.md"}})
	require.NoError(t, err)
	require.Empty(t, got)
}
