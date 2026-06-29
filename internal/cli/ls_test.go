package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// seedLsTree creates a small tree, ingests it, and leaves one indexable file
// un-indexed. It assumes the test has already chdir'd into a temp dir.
func seedLsTree(t *testing.T) {
	t.Helper()
	runCmd(t, "init")
	require.NoError(t, os.MkdirAll("adr", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("adr", "0001.md"),
		[]byte("---\ntype: adr\n---\n# One\n\nbody\n"), 0o644))
	require.NoError(t, os.WriteFile("readme.md", []byte("# Readme\n\nbody\n"), 0o644))
	runCmd(t, "ingest", ".", "--collection", "demo")
	// Indexable but never ingested.
	require.NoError(t, os.WriteFile("draft.md", []byte("# Draft\n\nbody\n"), 0o644))
}

func TestLsFlat(t *testing.T) {
	chdir(t, t.TempDir())
	seedLsTree(t)

	out := runCmd(t, "ls")
	require.Contains(t, out, "URI")
	require.Contains(t, out, "adr/0001.md")
	require.Contains(t, out, "readme.md")
	require.Contains(t, out, "draft.md")
	// adr is typed and indexed; draft is not indexed.
	require.Regexp(t, `adr/0001\.md\s+adr\s+demo\s+\S+\s+yes`, out)
	require.Regexp(t, `draft\.md\s+-\s+-\s+-\s+no`, out)
}

func TestLsUnindexedFilter(t *testing.T) {
	chdir(t, t.TempDir())
	seedLsTree(t)

	out := runCmd(t, "ls", "--unindexed")
	require.Contains(t, out, "draft.md")
	require.NotContains(t, out, "adr/0001.md")
}

func TestLsPathPrefixPositional(t *testing.T) {
	chdir(t, t.TempDir())
	seedLsTree(t)

	out := runCmd(t, "ls", "adr")
	require.Contains(t, out, "adr/0001.md")
	require.NotContains(t, out, "readme.md")
}

func TestLsTree(t *testing.T) {
	chdir(t, t.TempDir())
	seedLsTree(t)

	out := runCmd(t, "ls", "--tree")
	require.Contains(t, out, "adr/")
	require.Contains(t, out, "  0001.md")
	// Un-indexed file flagged with a trailing marker.
	require.Contains(t, out, "draft.md *")
}

func TestLsJSON(t *testing.T) {
	chdir(t, t.TempDir())
	seedLsTree(t)

	out := runCmd(t, "ls", "--json")
	require.Contains(t, out, `"uri": "adr/0001.md"`)
	require.Contains(t, out, `"indexed": true`)
	require.Contains(t, out, `"indexed": false`)
}

func TestLsMutuallyExclusiveFlags(t *testing.T) {
	chdir(t, t.TempDir())
	seedLsTree(t)

	_, err := runCmdErr(t, "ls", "--indexed", "--unindexed")
	require.Error(t, err)
	require.Contains(t, err.Error(), "mutually exclusive")
}
