package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/cli"
)

// runCmdErr executes the root command with args and returns the error (if any)
// along with captured output. Unlike runCmd it does not assert success.
func runCmdErr(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := cli.NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()

	return out.String(), err
}

// enableDelete writes a config in the current dir that turns on allow_delete
// while keeping all other defaults (koanf layers the file over the defaults).
func enableDelete(t *testing.T) {
	t.Helper()
	require.NoError(t, os.WriteFile(".mnemos.toml", []byte("[mcp]\nallow_delete = true\n"), 0o644))
}

func TestForgetCLIRemovesFileAndIndex(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	enableDelete(t)

	require.NoError(t, os.MkdirAll("tech", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("tech", "note.md"), []byte("# Note\n\nContent.\n"), 0o644))
	runCmd(t, "ingest", ".", "--collection", "tech")

	out := runCmd(t, "forget", "tech/note.md")
	require.Contains(t, out, "forgot tech/note.md")
	require.NoFileExists(t, filepath.Join("tech", "note.md"))

	status := runCmd(t, "status")
	require.Regexp(t, `documents\s+0`, status)
}

func TestForgetCLIRefusesWhenDeleteDisabled(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	out, err := runCmdErr(t, "forget", "anything.md")
	require.Error(t, err)
	require.Contains(t, err.Error(), "allow_delete")
	_ = out
}

func TestMvCLIMovesAndReindexes(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	enableDelete(t)

	require.NoError(t, os.MkdirAll("perso", 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("perso", "note.md"), []byte("# Note\n\nMovable.\n"), 0o644))
	runCmd(t, "ingest", ".", "--collection", "perso")

	out := runCmd(t, "mv", "perso/note.md", "tech/note.md")
	require.Contains(t, out, "moved perso/note.md -> tech/note.md")
	require.NoFileExists(t, filepath.Join("perso", "note.md"))
	require.FileExists(t, filepath.Join("tech", "note.md"))
}

func TestMvCLIMovesDirectory(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	enableDelete(t)

	require.NoError(t, os.MkdirAll(filepath.Join("adr", "sub"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join("adr", "one.md"), []byte("# One\n\nbody.\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join("adr", "sub", "two.md"), []byte("# Two\n\nbody.\n"), 0o644))
	runCmd(t, "ingest", ".", "--collection", "arch")

	out := runCmd(t, "mv", "adr", "archive")
	require.Contains(t, out, "moved adr/ -> archive/")
	require.Contains(t, out, "2 files re-indexed")
	require.NoDirExists(t, "adr")
	require.FileExists(t, filepath.Join("archive", "one.md"))
	require.FileExists(t, filepath.Join("archive", "sub", "two.md"))

	// Both files are searchable under their new uris.
	ls := runCmd(t, "ls", "--path", "archive", "--json")
	require.Contains(t, ls, "archive/one.md")
	require.Contains(t, ls, "archive/sub/two.md")
}

func TestMvCLIWarnsOnInboundLinks(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	enableDelete(t)

	require.NoError(t, os.WriteFile("target.md", []byte("# Target\n\nbody.\n"), 0o644))
	require.NoError(t, os.WriteFile("linker.md", []byte("# Linker\n\nSee [target](target.md).\n"), 0o644))
	runCmd(t, "ingest", ".", "--collection", "c")

	out := runCmd(t, "mv", "target.md", "moved.md")
	require.Contains(t, out, "moved target.md -> moved.md")
	require.Contains(t, out, "1 inbound link(s) still point at the old path")
}

func TestMvCLIRefusesWhenDeleteDisabled(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	_, err := runCmdErr(t, "mv", "a.md", "b.md")
	require.Error(t, err)
	require.Contains(t, err.Error(), "allow_delete")
}

func TestForgetCLIRejectsTraversal(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	enableDelete(t)

	_, err := runCmdErr(t, "forget", "../escape.md")
	require.Error(t, err)
	require.Contains(t, err.Error(), "forget:")
}

func TestMvCLIRejectsTraversalSource(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	enableDelete(t)

	_, err := runCmdErr(t, "mv", "../escape.md", "tech/x.md")
	require.Error(t, err)
	require.Contains(t, err.Error(), "mv: source:")
}

func TestMvCLIRejectsTraversalDest(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	enableDelete(t)

	require.NoError(t, os.WriteFile("a.md", []byte("# A\n\nbody.\n"), 0o644))
	runCmd(t, "ingest", ".", "--collection", "c")

	_, err := runCmdErr(t, "mv", "a.md", "../escape.md")
	require.Error(t, err)
	require.Contains(t, err.Error(), "mv: destination:")
}

func TestMvCLISourceNotIndexedUsesDefault(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	enableDelete(t)

	// File on disk but never ingested: mv relocates and indexes under "default".
	require.NoError(t, os.WriteFile("loose.md", []byte("# Loose\n\nbody.\n"), 0o644))

	out := runCmd(t, "mv", "loose.md", "kept.md")
	require.Contains(t, out, "moved loose.md -> kept.md")
	require.FileExists(t, "kept.md")
}

func TestForgetCLIIdempotentOnMissing(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	enableDelete(t)

	out := runCmd(t, "forget", "nope.md")
	require.Contains(t, out, "deleted from disk: false")
}
