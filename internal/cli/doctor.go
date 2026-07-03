package cli

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/doctor"
	"github.com/arhuman/mnemos/internal/memory"
)

// doctorFlags holds the values bound to the doctor command. The filter flag
// names mirror ls/search (--collection / --path) for consistency.
type doctorFlags struct {
	collection     string
	pathPrefix     string
	maxBytes       int64
	asJSON         bool
	failOnFindings bool
}

// newDoctorCmd builds the `doctor [path]` command: a read-only health report of
// the OKF tree (duplicates, broken links, oversized files, tag hygiene,
// structural gaps). It never mutates the store or files, so it needs no allow_*
// gate and is safe to run anywhere, including CI. The optional positional path is
// a uri prefix (equivalent to --path), matching ls.
func newDoctorCmd(state *rootState) *cobra.Command {
	var f doctorFlags
	cmd := &cobra.Command{
		Use:   "doctor [path]",
		Short: "Report knowledge-base health issues (read-only diagnosis)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				f.pathPrefix = args[0]
			}

			return runDoctor(cmd, state, f)
		},
	}
	cmd.Flags().StringVar(&f.collection, "collection", "", "restrict to a collection (exact match)")
	cmd.Flags().StringVar(&f.pathPrefix, "path", "", "restrict to a file or directory path prefix")
	cmd.Flags().Int64Var(&f.maxBytes, "max-bytes", doctor.DefaultMaxBytes, "flag documents larger than this many bytes")
	cmd.Flags().BoolVar(&f.asJSON, "json", false, "emit findings as a JSON array")
	cmd.Flags().BoolVar(&f.failOnFindings, "fail-on-findings", false, "exit non-zero when any finding is reported (for CI)")

	return cmd
}

func runDoctor(cmd *cobra.Command, state *rootState, f doctorFlags) error {
	// doctor is read-only (allowCreate=false): it must not create a database as a
	// side effect, and an absent store surfaces an actionable error rather than a
	// misleading empty report.
	return withStore(state, false, func(a *app.App) error {
		svc := memory.New(a.DB, a.Config, a.TreeRoot(), nil, a.Logger)
		findings, err := svc.Diagnose(cmd.Context(), doctor.Options{
			PathPrefix: f.pathPrefix,
			Collection: f.collection,
			MaxBytes:   f.maxBytes,
		})
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()
		if f.asJSON {
			if err := writeJSONFindings(out, findings); err != nil {
				return err
			}
		} else {
			renderFindings(out, findings)
		}

		if f.failOnFindings && len(findings) > 0 {
			return fmt.Errorf("doctor: %d finding(s)", len(findings))
		}

		return nil
	})
}

// categoryOrder fixes the display order of finding groups so output is stable and
// scannable regardless of detector execution order.
var categoryOrder = map[string]int{
	doctor.CategoryDuplicate:  0,
	doctor.CategoryBrokenLink: 1,
	doctor.CategoryOversized:  2,
	doctor.CategoryTagHygiene: 3,
	doctor.CategoryStructure:  4,
}

// renderFindings prints a tabular report ordered by category, then a summary.
func renderFindings(out io.Writer, findings []doctor.Finding) {
	if len(findings) == 0 {
		_, _ = fmt.Fprintln(out, "no issues found")

		return
	}

	sorted := make([]doctor.Finding, len(findings))
	copy(sorted, findings)
	slices.SortStableFunc(sorted, func(a, b doctor.Finding) int {
		return cmp.Compare(categoryOrder[a.Category], categoryOrder[b.Category])
	})

	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "SEVERITY\tCATEGORY\tISSUE\tDOCUMENTS")
	warn, info := 0, 0
	cats := make(map[string]bool)
	for _, fnd := range sorted {
		if fnd.Severity == doctor.SeverityWarn {
			warn++
		} else {
			info++
		}
		cats[fnd.Category] = true
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			fnd.Severity, fnd.Category, fnd.Title, formatURIs(fnd.URIs))
	}
	_ = w.Flush()

	_, _ = fmt.Fprintf(out, "\n%d finding(s) across %d categor%s: %d warn, %d info\n",
		len(findings), len(cats), plural(len(cats), "y", "ies"), warn, info)
}

// formatURIs renders a finding's documents compactly: up to three, then a count.
func formatURIs(uris []string) string {
	switch {
	case len(uris) == 0:
		return "-"
	case len(uris) <= 3:
		return strings.Join(uris, ", ")
	default:
		return fmt.Sprintf("%s (+%d more)", strings.Join(uris[:3], ", "), len(uris)-3)
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}

	return many
}

// writeJSONFindings emits the findings as an indented JSON array for CI and agent
// consumers (never null: an empty result is []).
func writeJSONFindings(out io.Writer, findings []doctor.Finding) error {
	if findings == nil {
		findings = []doctor.Finding{}
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(findings); err != nil {
		return fmt.Errorf("doctor: encode json: %w", err)
	}

	return nil
}
