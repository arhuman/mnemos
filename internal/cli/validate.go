package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/okf"
)

// newValidateCmd builds the `validate <bundle>` command. It runs the OKF v0.1
// conformance validator over a bundle directory and prints a report. The
// command opens no store, so on non-conformance it exits directly with status 1
// rather than returning an error (returning one would make main double-log).
// Returned errors are reserved for real filesystem failures.
func newValidateCmd(_ *rootState) *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "validate <bundle>",
		Short: "Validate an OKF v0.1 bundle for conformance",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rep, err := okf.Validate(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if jsonOut {
				if err := json.NewEncoder(cmd.OutOrStdout()).Encode(rep); err != nil {
					return err
				}
				if !rep.OK() {
					os.Exit(1)
				}

				return nil
			}
			printReport(cmd.OutOrStdout(), rep)
			if !rep.OK() {
				os.Exit(1)
			}

			return nil
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit the report as JSON")

	return cmd
}

// printReport renders a Report in the OKF skill's human format: a header line,
// one line per issue (errors first), and a tally footer. A clean bundle prints
// a single conformance line.
func printReport(w io.Writer, rep okf.Report) {
	if len(rep.Issues) == 0 {
		_, _ = fmt.Fprintf(w, "✅ OKF v0.1 conformant (%d concept files, 0 warnings)\n", rep.Files)

		return
	}

	_, _ = fmt.Fprintf(w, "%s: %d concept files\n", rep.Bundle, rep.Files)
	for _, sev := range []okf.Severity{okf.SevError, okf.SevWarning} {
		for _, iss := range rep.Issues {
			if iss.Severity != sev {
				continue
			}
			_, _ = fmt.Fprintf(w, "%s %s  %s  %s\n", severityIcon(iss.Severity), iss.Code, issueFile(iss), iss.Message)
		}
	}
	_, _ = fmt.Fprintf(w, "%d errors, %d warnings\n", rep.Errors, rep.Warnings)
}

// severityIcon maps a Severity to its display icon.
func severityIcon(s okf.Severity) string {
	if s == okf.SevError {
		return "❌"
	}

	return "⚠️ "
}

// issueFile returns a printable file label, using "(bundle)" for bundle-level
// issues that carry no file.
func issueFile(iss okf.Issue) string {
	if iss.File == "" {
		return "(bundle)"
	}

	return iss.File
}
