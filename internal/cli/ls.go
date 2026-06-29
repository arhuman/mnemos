package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/browse"
	"github.com/arhuman/mnemos/internal/memory"
)

// lsFlags holds the values bound to the ls command's filter and rendering flags.
// The filter flag names mirror search (--collection / --path / --type) for
// consistency.
type lsFlags struct {
	collection string
	pathPrefix string
	fileType   string
	tree       bool
	depth      int
	all        bool
	indexed    bool
	unindexed  bool
	limit      int
	asJSON     bool
}

// newLsCmd builds the `ls [path]` command, which lists the OKF tree by walking
// it on disk and annotating each file with the index metadata mnemos holds.
// It is read-only and needs no allow_* gate. The optional positional path is a
// uri prefix (equivalent to --path); the flag is accepted too for symmetry with
// search.
func newLsCmd(state *rootState) *cobra.Command {
	var f lsFlags
	cmd := &cobra.Command{
		Use:   "ls [path]",
		Short: "List and browse the OKF tree, annotated with index metadata",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				f.pathPrefix = args[0]
			}

			return runLs(cmd, state, f)
		},
	}
	cmd.Flags().StringVar(&f.collection, "collection", "", "restrict to a collection (exact match)")
	cmd.Flags().StringVar(&f.pathPrefix, "path", "", "restrict to a file or directory path (matched at segment boundaries)")
	cmd.Flags().StringVar(&f.fileType, "type", "", "restrict to a file extension (e.g. md)")
	cmd.Flags().BoolVar(&f.tree, "tree", false, "render the result as a directory tree")
	cmd.Flags().IntVar(&f.depth, "depth", 0, "in --tree mode, limit the tree to this many levels (0 = unlimited)")
	cmd.Flags().BoolVar(&f.all, "all", false, "include every file on disk, not just indexable ones")
	cmd.Flags().BoolVar(&f.indexed, "indexed", false, "only entries present in the index")
	cmd.Flags().BoolVar(&f.unindexed, "unindexed", false, "only entries not present in the index")
	cmd.Flags().IntVar(&f.limit, "limit", 0, "maximum number of entries (0 = unlimited)")
	cmd.Flags().BoolVar(&f.asJSON, "json", false, "emit entries as a JSON array")

	return cmd
}

func runLs(cmd *cobra.Command, state *rootState, f lsFlags) error {
	return withStore(state, false, func(a *app.App) error {
		svc := memory.New(a.DB, a.Config, a.TreeRoot(), nil, a.Logger)
		entries, err := svc.List(cmd.Context(), browse.Options{
			Collection:    f.collection,
			PathPrefix:    f.pathPrefix,
			FileType:      f.fileType,
			All:           f.all,
			IndexedOnly:   f.indexed,
			UnindexedOnly: f.unindexed,
			Limit:         f.limit,
		})
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()
		switch {
		case f.asJSON:
			return writeJSONEntries(out, entries)
		case f.tree:
			renderTree(out, browse.BuildTree(entries), f.depth)

			return nil
		default:
			renderEntries(out, entries)

			return nil
		}
	})
}

// renderEntries prints a tabular listing: uri, type, collection, modified, and
// the indexed flag. Un-indexed files show "-" for the index-only columns.
func renderEntries(out io.Writer, entries []browse.Entry) {
	if len(entries) == 0 {
		_, _ = fmt.Fprintln(out, "no entries")

		return
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "URI\tTYPE\tCOLLECTION\tMODIFIED\tINDEXED")
	for _, e := range entries {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			e.URI, dash(e.Type), dash(e.Collection), dash(e.ModifiedAt), yesNo(e.Indexed))
	}
	_ = w.Flush()
}

// renderTree prints the directory tree with two-space indentation per level.
// maxDepth > 0 limits how deep the tree is shown (0 = unlimited). Directory
// names carry a trailing slash; un-indexed files are flagged with " *".
func renderTree(out io.Writer, root *browse.TreeNode, maxDepth int) {
	if len(root.Children) == 0 {
		_, _ = fmt.Fprintln(out, "no entries")

		return
	}
	var walk func(n *browse.TreeNode, depth int)
	walk = func(n *browse.TreeNode, depth int) {
		if maxDepth > 0 && depth > maxDepth {
			return
		}
		indent := strings.Repeat("  ", depth-1)
		if n.IsDir {
			_, _ = fmt.Fprintf(out, "%s%s/\n", indent, n.Name)
		} else {
			label := n.Name
			if n.Entry != nil && !n.Entry.Indexed {
				label += " *"
			} else if n.Entry != nil && n.Entry.Type != "" {
				label += fmt.Sprintf(" (%s)", n.Entry.Type)
			}
			_, _ = fmt.Fprintf(out, "%s%s\n", indent, label)
		}
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	for _, c := range root.Children {
		walk(c, 1)
	}
}

// writeJSONEntries emits the entries as indented JSON for agent consumers.
func writeJSONEntries(out io.Writer, entries []browse.Entry) error {
	if entries == nil {
		entries = []browse.Entry{}
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(entries); err != nil {
		return fmt.Errorf("ls: encode json: %w", err)
	}

	return nil
}

func dash(s string) string {
	if s == "" {
		return "-"
	}

	return s
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}

	return "no"
}
