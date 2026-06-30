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

func TestAddCopiesDirectoryTree(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	ext := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ext, "sub"), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(ext, "top.md"), []byte("# Top\n\nbody one\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(ext, "sub", "nested.md"), []byte("# Nested\n\nbody two\n"), 0o644))

	out := runCmd(t, "add", ext, "--into", "vault", "--collection", "c")
	require.Contains(t, out, "files ingested:  2")
	require.FileExists(t, kbPath(filepath.Join("vault", "top.md")))
	require.FileExists(t, kbPath(filepath.Join("vault", "sub", "nested.md")))

	hit := runCmd(t, "search", "body two", "--json")
	require.Contains(t, hit, `"uri": "vault/sub/nested.md"`)
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

func TestAddLinkFileWorks(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	ext := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ext, "linked.md"), []byte("# Linked\n\nlinkable body omega\n"), 0o644))

	out := runCmd(t, "add", filepath.Join(ext, "linked.md"), "--mode", "link")
	require.Contains(t, out, "files ingested:  1")

	hit := runCmd(t, "search", "linkable body omega")
	require.Contains(t, hit, "linked.md")
}

func TestAddLinkDirRejected(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	ext := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ext, "a.md"), []byte("# A\n\nbody\n"), 0o644))

	_, err := runCmdErr(t, "add", ext, "--mode", "link")
	require.Error(t, err)
	require.Contains(t, err.Error(), "single file, not a directory")
}

func TestAddRejectsDestOutsideKB(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	ext := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ext, "n.md"), []byte("# N\n\nbody\n"), 0o644))

	_, err := runCmdErr(t, "add", filepath.Join(ext, "n.md"), "--into", "../escape.md")
	require.Error(t, err)
}

func TestAddUnknownMode(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	ext := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ext, "n.md"), []byte("# N\n\nbody\n"), 0o644))

	_, err := runCmdErr(t, "add", filepath.Join(ext, "n.md"), "--mode", "bogus")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown --mode")
}

func TestAddSourceNotFound(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	_, err := runCmdErr(t, "add", filepath.Join(t.TempDir(), "does-not-exist.md"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "source")
}

// TestAddCopyFileDestIsDir exercises copyFile's open-destination failure: when a
// directory already occupies the destination path, opening it for writing fails.
func TestAddCopyFileDestIsDir(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	// A directory already sits where the copied file would land.
	require.NoError(t, os.MkdirAll(kbPath("clash"), 0o750))

	ext := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ext, "f.md"), []byte("# F\n\nbody\n"), 0o644))

	_, err := runCmdErr(t, "add", filepath.Join(ext, "f.md"), "--into", "clash")
	require.Error(t, err)
	require.Contains(t, err.Error(), "copy")
}

// TestAddSourceFileUnreadable exercises copyFile's open-source failure: a source
// whose mode bits deny reads cannot be copied into the kb.
func TestAddSourceFileUnreadable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: file mode bits do not restrict access")
	}
	chdir(t, t.TempDir())
	runCmd(t, "init")

	ext := t.TempDir()
	src := filepath.Join(ext, "secret.md")
	require.NoError(t, os.WriteFile(src, []byte("# S\n\nbody\n"), 0o644))
	require.NoError(t, os.Chmod(src, 0o000))
	t.Cleanup(func() { _ = os.Chmod(src, 0o644) })

	_, err := runCmdErr(t, "add", src)
	require.Error(t, err)
	require.Contains(t, err.Error(), "copy")
}
