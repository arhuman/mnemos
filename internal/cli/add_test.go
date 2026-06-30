package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAddCopiesFileIntoKBAndIndexes(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	ext := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ext, "ext.md"), []byte("# Ext\n\nsearchable body here\n"), 0o644))

	out := runCmd(t, "add", filepath.Join(ext, "ext.md"))
	require.Contains(t, out, "files ingested:  1")
	// Content now lives inside the kb, and the original is left in place.
	require.FileExists(t, kbPath("ext.md"))
	require.FileExists(t, filepath.Join(ext, "ext.md"))

	s := runCmd(t, "search", "searchable body")
	require.Contains(t, s, "ext.md")
}

func TestAddIntoSubpath(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	ext := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ext, "n.md"), []byte("# N\n\nbody\n"), 0o644))

	runCmd(t, "add", filepath.Join(ext, "n.md"), "--into", filepath.Join("work", "n.md"))
	require.FileExists(t, kbPath(filepath.Join("work", "n.md")))
}

func TestAddMintsKBRelativeURIs(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	ext := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ext, "deep.md"), []byte("# Deep\n\nuniquephrase zeta\n"), 0o644))

	// Added under a subpath: the URI is relative to the kb root (work/...), not to
	// the scan root (which would drop the work/ prefix).
	runCmd(t, "add", filepath.Join(ext, "deep.md"), "--into", filepath.Join("work", "deep.md"))

	out := runCmd(t, "search", "uniquephrase zeta", "--json")
	require.Contains(t, out, `"uri": "work/deep.md"`)
}

func TestAddRejectsDestOutsideKB(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	ext := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ext, "n.md"), []byte("# N\n\nbody\n"), 0o644))

	_, err := runCmdErr(t, "add", filepath.Join(ext, "n.md"), "--into", "../escape.md")
	require.Error(t, err)
}
