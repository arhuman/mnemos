package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/okf"
)

// TestPrintReportConformant covers the zero-issues fast path that prints the
// "conformant" line with the file count.
func TestPrintReportConformant(t *testing.T) {
	rep := okf.Report{
		Bundle: "mybundle",
		Files:  5,
		Issues: nil,
	}
	var buf bytes.Buffer
	printReport(&buf, rep)
	out := buf.String()
	require.Contains(t, out, "conformant")
	require.Contains(t, out, "5")
}

// TestPrintReportWithIssues covers the branch that renders errors first, then
// warnings, using both severityIcon and issueFile internally.
func TestPrintReportWithIssues(t *testing.T) {
	rep := okf.Report{
		Bundle: "bad-bundle",
		Files:  3,
		Issues: []okf.Issue{
			{Code: "E001", Severity: okf.SevError, File: "foo.md", Message: "missing type"},
			{Code: "W001", Severity: okf.SevWarning, File: "", Message: "no description"},
		},
		Errors:   1,
		Warnings: 1,
	}
	var buf bytes.Buffer
	printReport(&buf, rep)
	out := buf.String()
	require.Contains(t, out, "bad-bundle")
	require.Contains(t, out, "E001")
	require.Contains(t, out, "W001")
	// Bundle-level issue (empty File) renders as "(bundle)".
	require.Contains(t, out, "(bundle)")
	// Error icon for E001, warning icon for W001.
	require.Contains(t, out, "❌")
	require.Contains(t, out, "1 errors")
	require.Contains(t, out, "1 warnings")
}

// TestSeverityIcon checks both branches: errors return the X icon, anything
// else returns the warning icon.
func TestSeverityIcon(t *testing.T) {
	require.Equal(t, "❌", severityIcon(okf.SevError))
	icon := severityIcon(okf.SevWarning)
	require.NotEqual(t, "❌", icon)
	require.NotEmpty(t, icon)
}

// TestIssueFile checks that an empty File produces "(bundle)" and a non-empty
// File is returned as-is.
func TestIssueFile(t *testing.T) {
	require.Equal(t, "(bundle)", issueFile(okf.Issue{File: ""}))
	require.Equal(t, "foo.md", issueFile(okf.Issue{File: "foo.md"}))
}
