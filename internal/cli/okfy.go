package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/memory"
)

// okfyOpts carries the parsed flags and source argument into runOkfy.
type okfyOpts struct {
	src        string
	collection string
	noteType   string
	tags       string
	out        string
	force      bool
}

// newOkfyCmd builds the `okfy <file>` command, which turns a plain .txt or .md
// file into an OKF document (frontmatter + body) at a chosen path and indexes
// it, leaving the source file intact.
func newOkfyCmd(state *rootState) *cobra.Command {
	opts := okfyOpts{}
	cmd := &cobra.Command{
		Use:   "okfy <file>",
		Short: "Convert a .txt or .md file into an OKF document and index it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.src = args[0]

			return runOkfy(cmd, state, opts)
		},
	}
	cmd.Flags().StringVar(&opts.collection, "collection", "default", "collection to index the OKF document under")
	cmd.Flags().StringVar(&opts.noteType, "type", "document", "OKF note type recorded in the frontmatter")
	cmd.Flags().StringVar(&opts.tags, "tags", "", "comma-separated frontmatter tags")
	cmd.Flags().StringVar(&opts.out, "out", "", "output path within the tree (defaults to the source path with a .md extension)")
	cmd.Flags().BoolVar(&opts.force, "force", false, "overwrite the output file if it already exists")

	return cmd
}

func runOkfy(cmd *cobra.Command, state *rootState, opts okfyOpts) error {
	return withStore(state, true, func(a *app.App) error {
		svc := memory.New(a.DB, a.Config, a.TreeRoot(), nil, a.Logger)
		res, err := svc.Okfy(cmd.Context(), memory.OkfyInput{
			Source:     opts.src,
			Out:        opts.out,
			Collection: opts.collection,
			Type:       opts.noteType,
			Tags:       parseTags(opts.tags),
			Force:      opts.force,
		})
		if err != nil {
			return err
		}

		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "okfied %s -> %s (collection %s, %d chunks, document %s)\n",
			res.SourceURI, res.URI, res.Collection, res.Chunks, res.DocumentID)

		return nil
	})
}

// parseTags splits a comma-separated tag string into a trimmed, non-empty slice.
func parseTags(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	tags := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			tags = append(tags, t)
		}
	}

	return tags
}
