package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReindexContentCLIRewritesDocuments exercises runReindex/reindexContent
// end-to-end: init, ingest a document, then reindex --content and assert the
// tally line it prints.
func TestReindexContentCLIRewritesDocuments(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	seedKB(t, "a.md", "# A\n\nsome body content\n")
	runCmd(t, "ingest", ".", "--collection", "demo")

	out := runCmd(t, "reindex", "--content")
	require.Contains(t, out, "content reindex:")
	require.Contains(t, out, "1/1 documents reparsed")
	require.Contains(t, out, "run 'reindex --embeddings' to rebuild them")
}

// TestReindexContentCLIMissingStore asserts reindex (read-only) refuses to
// create a database and reports the missing store clearly. HOME is isolated
// so workspace resolution cannot fall back to a real ~/.mnemos.
func TestReindexContentCLIMissingStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	chdir(t, t.TempDir())

	_, err := runCmdErr(t, "reindex", "--content")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no mnemos database")
}
