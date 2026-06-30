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

func TestMigrateSourceNotFound(t *testing.T) {
	to := filepath.Join(t.TempDir(), ".mnemos")
	_, err := runCmdErr(t, "migrate", "--from", filepath.Join(t.TempDir(), "nope"), "--to", to)
	require.Error(t, err)
	require.Contains(t, err.Error(), "--from")
}

func TestMigrateRejectsTargetKBEqualsSource(t *testing.T) {
	// --from is <dir>/kb and --to is <dir>, so the target kb (<dir>/kb) equals the
	// source root: migrate must refuse rather than relocate a tree onto itself.
	base := t.TempDir()
	src := filepath.Join(base, "kb")
	require.NoError(t, os.MkdirAll(src, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(src, "a.md"), []byte("# A\n"), 0o644))

	_, err := runCmdErr(t, "migrate", "--from", src, "--to", base)
	require.Error(t, err)
	require.Contains(t, err.Error(), "equals the source")
}

// TestMigrateFromConfigFileSkipsConfig exercises the --from-is-a-config-file
// branch: the file's directory is the tree root and the config itself is not
// relocated into the kb.
func TestMigrateFromConfigFileSkipsConfig(t *testing.T) {
	old := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(old, "old.toml"), []byte("[mcp]\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(old, "note.md"), []byte("# Note\n\nbody\n"), 0o644))
	to := filepath.Join(t.TempDir(), ".mnemos")

	runCmd(t, "migrate", "--from", filepath.Join(old, "old.toml"), "--to", to)

	require.FileExists(t, filepath.Join(to, "kb", "note.md"))
	require.NoFileExists(t, filepath.Join(to, "kb", "old.toml")) // the config is not relocated
}
