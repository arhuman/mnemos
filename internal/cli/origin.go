package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/storage"
)

// newOriginCmd builds the `origin` command group: register an external tree and
// index it in place, read-only, under a stable uri namespace (ADR-0013). It is
// the administration surface for content that stays canonical under its own
// tooling and must not be copied into the kb.
func newOriginCmd(state *rootState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "origin",
		Short: "Manage external trees indexed in place, read-only",
	}
	cmd.AddCommand(newOriginAddCmd(state))
	cmd.AddCommand(newOriginListCmd(state))
	cmd.AddCommand(newOriginReindexCmd(state))
	cmd.AddCommand(newOriginRemoveCmd(state))

	return cmd
}

func newOriginAddCmd(state *rootState) *cobra.Command {
	var prefix, collection string
	cmd := &cobra.Command{
		Use:   "add <absolute-dir>",
		Short: "Register an external directory and index it in place",
		Long: "Register an external directory. Its documents are indexed in place under\n" +
			"the uri namespace <prefix>/, never copied into the kb, and never written to.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(state, true, func(a *app.App) error {
				o, err := ingest.RegisterOrigin(cmd.Context(), a.DB, a.TreeRoot(), prefix, args[0], collection)
				if err != nil {
					return err
				}
				sum, err := reindexOneOrigin(cmd, a, o)
				if err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				_, _ = fmt.Fprintf(out, "registered:      %s -> %s\n", o.Prefix, o.Path)
				_, _ = fmt.Fprintf(out, "collection:      %s\n", o.Collection)
				printOriginSummary(out, sum)

				return nil
			})
		},
	}
	cmd.Flags().StringVar(&prefix, "prefix", "", "uri namespace for this origin's documents (required)")
	cmd.Flags().StringVar(&collection, "collection", "default", "fallback collection for documents without a collection: frontmatter")
	_ = cmd.MarkFlagRequired("prefix")

	return cmd
}

func newOriginListCmd(state *rootState) *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List registered origins",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withStore(state, false, func(a *app.App) error {
				origins, err := storage.ListOrigins(cmd.Context(), a.DB)
				if err != nil {
					return err
				}

				return printOrigins(cmd.OutOrStdout(), origins, asJSON)
			})
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON, so a registration set can be exported and replayed")

	return cmd
}

func newOriginReindexCmd(state *rootState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reindex [prefix]",
		Short: "Re-index registered origins in place (all, or one by prefix)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(state, false, func(a *app.App) error {
				origins, err := originsToReindex(cmd, a, args)
				if err != nil {
					return err
				}
				out := cmd.OutOrStdout()
				for _, o := range origins {
					sum, err := reindexOneOrigin(cmd, a, o)
					if err != nil {
						return err
					}
					_, _ = fmt.Fprintf(out, "origin:          %s (%s)\n", o.Prefix, o.Path)
					printOriginSummary(out, sum)
				}

				return nil
			})
		},
	}

	return cmd
}

func newOriginRemoveCmd(state *rootState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <prefix>",
		Short: "Unregister an origin and evict its documents (the files are untouched)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withStore(state, false, func(a *app.App) error {
				removed, err := ingest.RemoveOrigin(cmd.Context(), a.DB, args[0])
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintf(cmd.OutOrStdout(),
					"removed:         origin %q, %d documents evicted (files on disk untouched)\n", args[0], removed)

				return nil
			})
		},
	}

	return cmd
}

// originsToReindex resolves the command's argument to the origins to process.
func originsToReindex(cmd *cobra.Command, a *app.App, args []string) ([]storage.Origin, error) {
	if len(args) == 1 {
		o, err := storage.GetOrigin(cmd.Context(), a.DB, args[0])
		if err != nil {
			return nil, err
		}

		return []storage.Origin{o}, nil
	}

	return storage.ListOrigins(cmd.Context(), a.DB)
}

// reindexOneOrigin runs the pipeline over one origin with the configured rules.
func reindexOneOrigin(cmd *cobra.Command, a *app.App, o storage.Origin) (ingest.OriginReindexSummary, error) {
	return ingest.New(a.DB, a.Logger, pipelineOptions(a)...).ReindexOrigin(cmd.Context(), o,
		ingest.Rules{
			Include:         a.Config.Indexing.Include,
			Exclude:         a.Config.Indexing.Exclude,
			SecurityExclude: a.Config.SecurityExclude(),
		},
		chunk.ConfigFrom(a.Config.Chunking.TargetTokens, a.Config.Chunking.OverlapTokens))
}

func printOriginSummary(out io.Writer, sum ingest.OriginReindexSummary) {
	_, _ = fmt.Fprintf(out, "files scanned:   %d\n", sum.Scanned)
	_, _ = fmt.Fprintf(out, "files indexed:   %d\n", sum.Indexed)
	_, _ = fmt.Fprintf(out, "files skipped:   %d\n", sum.Skipped)
	if sum.Removed > 0 {
		_, _ = fmt.Fprintf(out, "files removed:   %d (backing file gone)\n", sum.Removed)
	}
	_, _ = fmt.Fprintf(out, "chunks written:  %d\n", sum.Chunks)
}

func printOrigins(out io.Writer, origins []storage.Origin, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")

		return enc.Encode(originsJSON(origins))
	}
	if len(origins) == 0 {
		_, _ = fmt.Fprintln(out, "no registered origins")

		return nil
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PREFIX\tPATH\tCOLLECTION\tLAST INDEXED\tSTATUS")
	for _, o := range origins {
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			o.Prefix, o.Path, o.Collection, orDash(o.LastIndexedAt), originStatus(o))
	}

	return w.Flush()
}

// originJSON is the exported shape of a registration: enough to re-create it.
type originJSON struct {
	Prefix        string `json:"prefix"`
	Path          string `json:"path"`
	Collection    string `json:"collection"`
	RegisteredAt  string `json:"registered_at"`
	LastIndexedAt string `json:"last_indexed_at,omitempty"`
	Status        string `json:"status"`
}

func originsJSON(origins []storage.Origin) []originJSON {
	out := make([]originJSON, 0, len(origins))
	for _, o := range origins {
		out = append(out, originJSON{
			Prefix: o.Prefix, Path: o.Path, Collection: o.Collection,
			RegisteredAt: o.RegisteredAt, LastIndexedAt: o.LastIndexedAt,
			Status: originStatus(o),
		})
	}

	return out
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}

	return s
}

// originStatus reports whether the origin's root is currently readable. A missing
// root is the condition that makes a reindex refuse, so it is surfaced in the
// listing rather than discovered by running one.
func originStatus(o storage.Origin) string {
	if _, err := os.Stat(o.Path); err != nil {
		return "MISSING"
	}

	return "ok"
}
