package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// goodBundle resolves the absolute path to the conformant OKF testdata bundle
// relative to this package's source directory. Tests run with cwd set to the
// package directory, so the caller must capture the cwd before any chdir call.
func goodBundle(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)

	return filepath.Join(cwd, "..", "okf", "testdata", "good")
}

// warnBundle returns a warning-only (conformant) OKF bundle.
func warnBundle(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)

	return filepath.Join(cwd, "..", "okf", "testdata", "w1")
}

// TestValidateConformantBundle runs `validate` against the good testdata bundle
// and asserts the conformance line is printed and the command succeeds.
func TestValidateConformantBundle(t *testing.T) {
	bundle := goodBundle(t)

	out := runCmd(t, "validate", bundle)
	require.Contains(t, out, "conformant")
}

// TestValidateJSONConformantBundle exercises the --json flag against a
// conformant bundle: the output must be valid JSON containing the expected keys
// and the command must succeed (rep.OK() is true so os.Exit is never called).
func TestValidateJSONConformantBundle(t *testing.T) {
	bundle := goodBundle(t)

	out := runCmd(t, "validate", bundle, "--json")
	require.Contains(t, out, `"bundle"`)
	require.Contains(t, out, `"files"`)
	require.Contains(t, out, `"errors":0`)
	require.Contains(t, out, `"warnings":0`)
}

// TestValidateWarningBundle checks that a bundle with warnings (but no errors)
// prints the issues table, exercises printReport's issue-rendering branch, and
// still returns no error (warnings do not break conformance).
func TestValidateWarningBundle(t *testing.T) {
	bundle := warnBundle(t)

	out := runCmd(t, "validate", bundle)
	// Warning bundles print an issue table ending with the tally line.
	require.Contains(t, out, "warnings")
}

// TestValidateNonExistentBundle asserts that a missing path propagates a
// filesystem error back through RunE (not os.Exit).
func TestValidateNonExistentBundle(t *testing.T) {
	_, err := runCmdErr(t, "validate", "/no/such/bundle/dir")
	require.Error(t, err)
}

// TestValidateMissingArg exercises cobra's ExactArgs(1) guard.
func TestValidateMissingArg(t *testing.T) {
	_, err := runCmdErr(t, "validate")
	require.Error(t, err)
}
