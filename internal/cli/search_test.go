package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSearchCommand ingests a tiny corpus then exercises the search command:
// the default citation format, the --json flag, and a punctuation-heavy query.
func TestSearchCommand(t *testing.T) {
	workdir := t.TempDir()
	chdir(t, workdir)

	src := filepath.Join(workdir, "src")
	writeTree(t, src, "docs/scim.md",
		"# SCIM\n\n## Provisioning\n\nSCIM provisioning with Entra synchronizes users automatically.\n")
	writeTree(t, src, "docs/cooking.md",
		"# Cooking\n\n## Pasta\n\nBoil water and cook pasta.\n")

	runCmd(t, "init")
	runCmd(t, "ingest", "src", "--collection", "docs")

	t.Run("citation format", func(t *testing.T) {
		out := runCmd(t, "search", "SCIM", "provisioning", "Entra")
		require.Contains(t, out, "1. docs/scim.md#Provisioning")
		require.Contains(t, out, "lines ")
		require.Contains(t, out, "score ")
	})

	t.Run("json output", func(t *testing.T) {
		out := runCmd(t, "search", "SCIM", "provisioning", "--json", "--limit", "1")
		require.Contains(t, out, `"uri": "docs/scim.md"`)
	})

	t.Run("punctuation-heavy query does not error", func(t *testing.T) {
		out := runCmd(t, "search", "SCIM:", "provisioning", "(Entra)?")
		require.Contains(t, out, "docs/scim.md")
	})

	t.Run("collection filter narrows", func(t *testing.T) {
		out := runCmd(t, "search", "SCIM", "--collection", "no-such")
		require.Contains(t, out, "no results")
	})
}

// TestEvalCommand runs the eval command against the eval package's testdata
// bundle and asserts the metrics table is rendered.
func TestEvalCommand(t *testing.T) {
	// Resolve the bundle path against the original cwd before chdir moves it.
	cwd, err := os.Getwd()
	require.NoError(t, err)
	bundle := filepath.Join(cwd, "..", "eval", "testdata", "bundle")

	workdir := t.TempDir()
	chdir(t, workdir)
	runCmd(t, "init")

	out := runCmd(t, "eval", bundle, "--baseline", filepath.Join(workdir, "baseline.json"), "--save")
	require.Contains(t, out, "queries     2")
	require.Contains(t, out, "Hit@1")
	require.Contains(t, out, "Recall@12")
	require.Contains(t, out, "MRR@12")
	require.Contains(t, out, "saved baseline")
}
