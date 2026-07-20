package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/embed"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/search"
)

// reindexFlags holds the values bound to the reindex command's flags.
type reindexFlags struct {
	embeddings bool
	content    bool
}

// newReindexCmd builds the `reindex` command. `--content` re-parses and rewrites
// every already-indexed document from its on-disk file (propagating a parser or
// schema change without editing files); `--embeddings` (re)computes a vector for
// every chunk. Both may be combined; content runs first so embeddings rebuild on
// the fresh chunks. The default build, compiled without embedding support, prints
// a clear message for --embeddings instead of failing.
func newReindexCmd(state *rootState) *cobra.Command {
	var f reindexFlags
	cmd := &cobra.Command{
		Use:   "reindex",
		Short: "Recompute derived indexes (document content and/or embeddings)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReindex(cmd, state, f)
		},
	}
	cmd.Flags().BoolVar(&f.content, "content", false, "re-parse and rewrite every indexed document from its file (bypasses the unchanged-file skip)")
	cmd.Flags().BoolVar(&f.embeddings, "embeddings", false, "recompute and store embedding vectors for all chunks")

	return cmd
}

func runReindex(cmd *cobra.Command, state *rootState, f reindexFlags) error {
	if !f.embeddings && !f.content {
		return errors.New("reindex: nothing to do (pass --content and/or --embeddings)")
	}
	if f.embeddings && !embed.Supported {
		return fmt.Errorf("reindex --embeddings: %s", noEmbedSupportMsg)
	}

	return withStore(state, false, func(a *app.App) error {
		// Content first: it rewrites chunks (cascading away their embeddings), so a
		// combined run must rebuild embeddings on the fresh chunks afterwards.
		if f.content {
			if err := reindexContent(cmd, a); err != nil {
				return err
			}
		}
		if f.embeddings {
			if err := reindexEmbeddings(cmd, a); err != nil {
				return err
			}
		}

		return nil
	})
}

// reindexContent re-parses every indexed document from disk and reports the tally.
func reindexContent(cmd *cobra.Command, a *app.App) error {
	cfg := chunk.ConfigFrom(a.Config.Chunking.TargetTokens, a.Config.Chunking.OverlapTokens)
	sum, err := ingest.New(a.DB, a.Logger, ingest.WithMaxFileBytes(a.Config.Indexing.MaxFileBytes)).
		ReindexContent(cmd.Context(), a.TreeRoot(), cfg)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "content reindex: %d/%d documents reparsed (%d chunks; %d skipped, %d missing)\n",
		sum.Reindexed, sum.Documents, sum.Chunks, sum.Skipped, sum.Missing)
	if sum.Reindexed > 0 {
		_, _ = fmt.Fprintln(out, "note: rewritten chunks dropped their embeddings; run 'reindex --embeddings' to rebuild them")
	}

	return nil
}

// reindexEmbeddings recomputes and stores a vector for every chunk.
func reindexEmbeddings(cmd *cobra.Command, a *app.App) error {
	e, err := loadEmbedder()
	if err != nil {
		return err
	}

	count, err := search.Reindex(cmd.Context(), a.DB, e, a.Logger)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "reindexed %d embedding vectors (model %s)\n", count, e.Model())

	return nil
}
