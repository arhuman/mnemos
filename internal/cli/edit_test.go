package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// enableWrite overwrites the workspace config to turn on allow_write while
// keeping all other defaults (koanf layers the file over the defaults).
func enableWrite(t *testing.T) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(".mnemos", "mnemos.toml"), []byte("[mcp]\nallow_write = true\n"), 0o644))
}

// TestEditCLIRefusesWhenWriteDisabled proves the gate fires before any terminal
// program is started, so the refusal never depends on a tty.
func TestEditCLIRefusesWhenWriteDisabled(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	_, err := runCmdErr(t, "edit", "anything.md")
	require.Error(t, err)
	require.Contains(t, err.Error(), "allow_write")
}

// TestEditCLIRequiresAURI proves the missing-argument case is a clear message
// rather than a picker that does not exist yet.
func TestEditCLIRequiresAURI(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	enableWrite(t)

	_, err := runCmdErr(t, "edit")
	require.Error(t, err)
	require.Contains(t, err.Error(), "provide a uri")
}

// TestEditCLIRejectsTooManyArgs proves the arity is enforced by cobra.
func TestEditCLIRejectsTooManyArgs(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	enableWrite(t)

	_, err := runCmdErr(t, "edit", "a.md", "b.md")
	require.Error(t, err)
}

// TestEditCLIRejectsUnknownURI proves an unresolvable uri fails while the model
// is being constructed, before the program ever attaches to a terminal.
func TestEditCLIRejectsUnknownURI(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	enableWrite(t)

	_, err := runCmdErr(t, "edit", "tasks/nope.md")
	require.Error(t, err)
	require.Contains(t, err.Error(), "edit read")
}

// TestEditCLIRejectsPathOutsideTree proves confinement is enforced before the
// editor opens.
func TestEditCLIRejectsPathOutsideTree(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	enableWrite(t)

	_, err := runCmdErr(t, "edit", "../escape.md")
	require.Error(t, err)
	require.Contains(t, err.Error(), "edit path")
}
