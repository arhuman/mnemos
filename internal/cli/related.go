package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/memory"
)

// relatedFlags holds the values bound to the related command's flags.
type relatedFlags struct {
	direction string
	limit     int
	asJSON    bool
}

// newRelatedCmd builds the `related <uri>` command, which lists the 1-hop
// link-graph neighbors of a document: outbound links it contains and inbound
// backlinks that point at it. It is read-only and needs no allow_* gate.
func newRelatedCmd(state *rootState) *cobra.Command {
	var f relatedFlags
	cmd := &cobra.Command{
		Use:   "related <uri>",
		Short: "List a document's link-graph neighbors (outbound links and inbound backlinks)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRelated(cmd, state, args[0], f)
		},
	}
	cmd.Flags().StringVar(&f.direction, "direction", "both", "which edges to follow: outbound, inbound, or both")
	cmd.Flags().IntVar(&f.limit, "limit", 0, "maximum neighbors per direction (0 = default)")
	cmd.Flags().BoolVar(&f.asJSON, "json", false, "emit the neighborhood as JSON")

	return cmd
}

func runRelated(cmd *cobra.Command, state *rootState, uri string, f relatedFlags) error {
	return withStore(state, false, func(a *app.App) error {
		svc := memory.New(a.DB, a.Config, a.TreeRoot(), nil, a.Logger)
		res, err := svc.Related(cmd.Context(), uri, memory.Direction(f.direction), f.limit)
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()
		if f.asJSON {
			return writeJSONRelated(out, res)
		}
		renderRelated(out, res)

		return nil
	})
}

// renderRelated prints a tabular listing of the neighborhood: direction, uri,
// title, collection, and whether the target is indexed (resolved).
func renderRelated(out io.Writer, res memory.RelatedResult) {
	if len(res.Outbound) == 0 && len(res.Inbound) == 0 {
		_, _ = fmt.Fprintln(out, "no neighbors")

		return
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "DIRECTION\tURI\tTITLE\tCOLLECTION\tRESOLVED")
	for _, n := range res.Outbound {
		writeRelatedRow(w, n)
	}
	for _, n := range res.Inbound {
		writeRelatedRow(w, n)
	}
	_ = w.Flush()
}

func writeRelatedRow(w io.Writer, n memory.RelatedNeighbor) {
	_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
		n.Direction, n.URI, dash(n.Title), dash(n.Collection), yesNo(n.Resolved))
}

// writeJSONRelated emits the neighborhood as indented JSON for agent consumers,
// normalizing nil direction slices to empty so both arrays are always present.
func writeJSONRelated(out io.Writer, res memory.RelatedResult) error {
	if res.Outbound == nil {
		res.Outbound = []memory.RelatedNeighbor{}
	}
	if res.Inbound == nil {
		res.Inbound = []memory.RelatedNeighbor{}
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		return fmt.Errorf("related: encode json: %w", err)
	}

	return nil
}
