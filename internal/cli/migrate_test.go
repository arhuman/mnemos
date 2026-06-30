package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// oldWorkspace builds a pre-MNEMOS_DIR layout: content at the tree root and
// capture notes under the old internal .mnemos/capture directory.
func oldWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "pro", "epfl"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, "pro", "epfl", "note.md"),
		[]byte("---\ntype: note\ncollection: epfl\n---\n# Note\n\nmigratable body content\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".mnemos", "capture"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".mnemos", "capture", "idea.md"),
		[]byte("---\ntype: idea\n---\n# Idea\n\ncaptured note\n"), 0o644))

	return root
}

func TestMigrateCopiesIntoKBAndReindexes(t *testing.T) {
	old := oldWorkspace(t)
	to := filepath.Join(t.TempDir(), ".mnemos")

	out := runCmd(t, "migrate", "--from", old, "--to", to)
	require.Contains(t, out, "reindexed:")

	// Content relocated under kb/, old capture under kb/capture.
	require.FileExists(t, filepath.Join(to, "kb", "pro", "epfl", "note.md"))
	require.FileExists(t, filepath.Join(to, "kb", "capture", "idea.md"))
	require.FileExists(t, filepath.Join(to, "state", "index.db"))

	// Copy (the default) leaves the source intact.
	require.FileExists(t, filepath.Join(old, "pro", "epfl", "note.md"))

	// The document is searchable under the new workspace, and its collection:
	// frontmatter (epfl) survived the reindex.
	hit := runCmd(t, "search", "migratable body content", "--mnemos-dir", to, "--collection", "epfl")
	require.Contains(t, hit, "note.md")
}

func TestMigrateMoveLeavesSourceEmpty(t *testing.T) {
	old := oldWorkspace(t)
	to := filepath.Join(t.TempDir(), ".mnemos")

	runCmd(t, "migrate", "--from", old, "--to", to, "--move")

	require.FileExists(t, filepath.Join(to, "kb", "pro", "epfl", "note.md"))
	// Moved: the source content is gone (the old .mnemos dir is left behind).
	require.NoDirExists(t, filepath.Join(old, "pro"))
}

func TestMigrateRequiresFrom(t *testing.T) {
	_, err := runCmdErr(t, "migrate")
	require.Error(t, err)
	require.Contains(t, err.Error(), "--from is required")
}
