package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/embed"
	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/search"
)

// searchFlags holds the values bound to the search command's filter flags.
type searchFlags struct {
	collection string
	pathPrefix string
	fileType   string
	since      string
	limit      int
	asJSON     bool
	semantic   bool
}

// newSearchCmd builds the `search <query...>` command. The query words are
// joined with spaces, sanitized into an FTS5 MATCH expression, and ranked by the
// FTS5 engine. Filter flags narrow results to a collection, path prefix, file
// type, or modification date.
func newSearchCmd(state *rootState) *cobra.Command {
	var f searchFlags
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the index and print cited, ranked results",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSearch(cmd, state, args, f)
		},
	}
	cmd.Flags().StringVar(&f.collection, "collection", "", "restrict to a collection (exact match)")
	cmd.Flags().StringVar(&f.pathPrefix, "path", "", "restrict to documents whose uri starts with this prefix")
	cmd.Flags().StringVar(&f.fileType, "type", "", "restrict to a file extension (e.g. md)")
	cmd.Flags().StringVar(&f.since, "since", "", "restrict to documents modified at or after this RFC3339 timestamp")
	cmd.Flags().IntVar(&f.limit, "limit", 0, "maximum number of results (default: config search.default_limit)")
	cmd.Flags().BoolVar(&f.asJSON, "json", false, "emit results as a JSON array")
	cmd.Flags().BoolVar(&f.semantic, "semantic", false, "fuse lexical and vector retrieval (requires -tags embed build and an installed model)")

	return cmd
}

func runSearch(cmd *cobra.Command, state *rootState, args []string, f searchFlags) error {
	return withStore(state, false, func(a *app.App) error {
		retriever, err := buildRetriever(cmd, a, f.semantic)
		if err != nil {
			return err
		}

		svc := memory.New(a.DB, a.Config, a.TreeRoot(), nil, a.Logger)
		results, err := svc.Search(cmd.Context(), retriever, search.Query{
			Text:          strings.Join(args, " "),
			Collection:    f.collection,
			PathPrefix:    f.pathPrefix,
			FileType:      f.fileType,
			ModifiedSince: f.since,
			Limit:         f.limit,
		})
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()
		if f.asJSON {
			return writeJSONResults(out, results)
		}
		writeCitations(out, results)

		return nil
	})
}

// buildRetriever returns the Retriever for a search. Without --semantic it is
// the lexical FTS engine. With --semantic it fuses lexical and vector retrieval
// via RRF — but a binary built without embedding support, or one with no model
// installed, prints a warning to stderr and degrades to lexical-only rather than
// failing, honoring the "useful without embeddings" design principle.
func buildRetriever(cmd *cobra.Command, a *app.App, semantic bool) (search.Retriever, error) {
	engine := search.NewEngine(a.DB, a.Logger)
	if !semantic {
		return engine, nil
	}
	if !embed.Supported {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s; falling back to lexical search\n", noEmbedSupportMsg)

		return engine, nil
	}
	e, err := loadEmbedder()
	if err != nil {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %v; falling back to lexical search\n", err)

		return engine, nil
	}
	vector := search.NewVectorRetriever(a.DB, e, a.Logger)

	return search.NewHybridRetriever(engine, vector, a.Logger), nil
}

// writeCitations renders results in the plan's citation style:
//
//  1. uri#Heading
//     lines 42-88
//     score 12.7
//
// The heading shown is the last segment of the chunk's heading path; results
// with no heading omit the "#…" suffix.
func writeCitations(out io.Writer, results []model.Result) {
	if len(results) == 0 {
		_, _ = fmt.Fprintln(out, "no results")

		return
	}
	for i, r := range results {
		_, _ = fmt.Fprintf(out, "%d. %s\n", i+1, citation(r))
		_, _ = fmt.Fprintf(out, "   lines %d-%d\n", r.StartLine, r.EndLine)
		_, _ = fmt.Fprintf(out, "   score %.1f\n", r.Score)
		if i < len(results)-1 {
			_, _ = fmt.Fprintln(out)
		}
	}
}

// citation builds the "uri#Heading" line: the document uri plus the last
// segment of the heading path. An empty heading yields just the uri.
func citation(r model.Result) string {
	heading := model.LastHeading(r.HeadingPath)
	if heading == "" {
		return r.URI
	}

	return r.URI + "#" + heading
}

// writeJSONResults emits the results array as indented JSON for agent consumers.
func writeJSONResults(out io.Writer, results []model.Result) error {
	if results == nil {
		results = []model.Result{}
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(results); err != nil {
		return fmt.Errorf("search: encode json: %w", err)
	}

	return nil
}
