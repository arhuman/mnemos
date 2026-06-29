package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/embed"
	"github.com/arhuman/mnemos/internal/search"
)

// reindexFlags holds the values bound to the reindex command's flags.
type reindexFlags struct {
	embeddings bool
}

// newReindexCmd builds the `reindex` command. Phase 4 supports `--embeddings`:
// (re)compute and store a vector for every chunk. The default build, compiled
// without embedding support, prints a clear rebuild message instead of failing.
func newReindexCmd(state *rootState) *cobra.Command {
	var f reindexFlags
	cmd := &cobra.Command{
		Use:   "reindex",
		Short: "Recompute derived indexes (currently: embeddings)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runReindex(cmd, state, f)
		},
	}
	cmd.Flags().BoolVar(&f.embeddings, "embeddings", false, "recompute and store embedding vectors for all chunks")

	return cmd
}

func runReindex(cmd *cobra.Command, state *rootState, f reindexFlags) error {
	if !f.embeddings {
		return errors.New("reindex: nothing to do (pass --embeddings)")
	}
	if !embed.Supported {
		return fmt.Errorf("reindex --embeddings: %s", noEmbedSupportMsg)
	}

	return withStore(state, false, func(a *app.App) error {
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
	})
}
