package cli_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDoctorCLIReportsNoIssues exercises runDoctor end-to-end (the RunE wiring
// through withStore and memory.Diagnose) on an empty, freshly-initialized
// store: no documents means no findings of any category.
func TestDoctorCLIReportsNoIssues(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	out := runCmd(t, "doctor")
	require.Contains(t, out, "no issues found")
}

// TestDoctorCLIJSONAndFailOnFindings exercises the --json output path and the
// --fail-on-findings exit code on a tree with a detectable issue (a broken
// link).
func TestDoctorCLIJSONAndFailOnFindings(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	seedKB(t, "a.md", "# A\n\ndangling [link](missing.md)\n")
	runCmd(t, "ingest", ".", "--collection", "demo")

	jsonOut := runCmd(t, "doctor", "--json")
	var findings []map[string]any
	require.NoError(t, json.Unmarshal([]byte(jsonOut), &findings))
	require.NotEmpty(t, findings)

	_, err := runCmdErr(t, "doctor", "--fail-on-findings")
	require.Error(t, err)
	require.Contains(t, err.Error(), "finding(s)")
}

// TestDoctorCLIMissingStore asserts the read-only gate: doctor must not create
// a database and must surface an actionable error when none exists. HOME is
// isolated so workspace resolution cannot fall back to a real ~/.mnemos.
func TestDoctorCLIMissingStore(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	chdir(t, t.TempDir())

	_, err := runCmdErr(t, "doctor")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no mnemos database")
}
