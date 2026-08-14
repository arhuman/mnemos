package cli_test

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestServeCLIMissingStore exercises runServe's early error path (withStore
// failing because no database exists yet) without ever reaching srv.Serve,
// which blocks forever reading stdio. HOME is isolated so workspace resolution
// cannot fall back to a real ~/.mnemos.
func TestServeCLIMissingStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	chdir(t, t.TempDir())

	_, err := runCmdErr(t, "serve")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no mnemos database")
}

// TestServeCLIRejectsArgs proves the command takes no positional arguments.
// HOME is isolated for the same reason as TestServeCLIMissingStore.
func TestServeCLIRejectsArgs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	chdir(t, t.TempDir())

	_, err := runCmdErr(t, "serve", "unexpected")
	require.Error(t, err)
}
