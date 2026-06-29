//go:build !embed

package cli_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/cli"
)

// execErr runs the root command with args and returns combined output and the
// execution error (the success-only runCmd helper requires NoError).
func execErr(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := cli.NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()

	return out.String(), err
}

func TestReindexWithoutFlag(t *testing.T) {
	_, err := execErr(t, "reindex")
	require.Error(t, err)
	require.Contains(t, err.Error(), "nothing to do")
}

func TestReindexEmbeddingsDefaultBuild(t *testing.T) {
	// The default (no embed tag) build cannot embed; it must say so clearly
	// rather than fail to compile or silently no-op.
	_, err := execErr(t, "reindex", "--embeddings")
	require.Error(t, err)
	require.Contains(t, err.Error(), "rebuild with -tags embed")
}

func TestSearchSemanticDefaultBuildFallsBack(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	out, err := execErr(t, "search", "anything", "--semantic")
	require.NoError(t, err, "semantic search must degrade gracefully in the default build")
	require.Contains(t, out, "built without embedding support")
	require.Contains(t, out, "falling back to lexical")
}
