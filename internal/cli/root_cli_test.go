package cli_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/cli"
)

// TestExecuteRunsRootCommand exercises cli.Execute(), the single entry point
// used by main, which runCmd bypasses by calling NewRootCmd directly.
func TestExecuteRunsRootCommand(t *testing.T) {
	prevArgs := os.Args
	t.Cleanup(func() { os.Args = prevArgs })

	os.Args = []string{"mnemos", "version"}
	require.NoError(t, cli.Execute())
}

// TestExecuteReturnsCommandError proves Execute propagates a subcommand's
// error rather than swallowing it (SilenceErrors keeps cobra from also
// printing it, but the error itself must still reach the caller).
func TestExecuteReturnsCommandError(t *testing.T) {
	prevArgs := os.Args
	t.Cleanup(func() { os.Args = prevArgs })

	os.Args = []string{"mnemos", "reindex"}
	err := cli.Execute()
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "nothing to do"))
}
