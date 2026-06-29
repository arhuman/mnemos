package cli_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestVersion asserts the plain form prints a single "mnemos <version>" line and
// the -v form adds the build metadata and the embeddings build status.
func TestVersion(t *testing.T) {
	plain := runCmd(t, "version")
	require.True(t, strings.HasPrefix(plain, "mnemos "), "want leading 'mnemos ', got %q", plain)
	require.NotContains(t, strings.TrimSpace(plain), "\n", "plain version should be one line: %q", plain)

	verbose := runCmd(t, "version", "-v")
	for _, want := range []string{"version:", "commit:", "built:", "go:", "embeddings:"} {
		require.Contains(t, verbose, want)
	}
}
