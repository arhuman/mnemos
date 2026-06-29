package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/embed"
	"github.com/arhuman/mnemos/internal/eval"
)

// evalFlags holds the values bound to the eval command's flags.
type evalFlags struct {
	baseline string
	save     bool
	limit    int
	semantic bool
}

// newEvalCmd builds the `eval <bundle>` command. It runs the OKF held-out
// retrieval evaluation entirely in an ephemeral database, never touching the
// user's real store, and prints metrics with deltas against a baseline.
func newEvalCmd(state *rootState) *cobra.Command {
	var f evalFlags
	cmd := &cobra.Command{
		Use:   "eval <bundle>",
		Short: "Evaluate retrieval quality on an OKF bundle (held-out, doc-level)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEval(cmd, state, args[0], f)
		},
	}
	cmd.Flags().StringVar(&f.baseline, "baseline", "", "path to baseline.json (default: <bundle>/baseline.json)")
	cmd.Flags().BoolVar(&f.save, "save", false, "write current metrics to the baseline path")
	cmd.Flags().IntVar(&f.limit, "limit", 0, "retrieval depth K (default: 12)")
	cmd.Flags().BoolVar(&f.semantic, "semantic", false, "evaluate the hybrid retriever (requires -tags embed build and an installed model)")

	return cmd
}

func runEval(cmd *cobra.Command, state *rootState, bundle string, f evalFlags) error {
	a, err := state.loadApp()
	if err != nil {
		return err
	}

	baselinePath := f.baseline
	if baselinePath == "" {
		baselinePath = filepath.Join(bundle, "baseline.json")
	}

	var modelDir string
	if f.semantic {
		if !embed.Supported {
			return fmt.Errorf("eval --semantic: %s", noEmbedSupportMsg)
		}
		modelDir, err = embed.ModelDir(embed.DefaultModel)
		if err != nil {
			return err
		}
	}

	opts := eval.Options{
		Bundle:   bundle,
		K:        f.limit,
		Include:  a.Config.Indexing.Include,
		Exclude:  a.Config.Indexing.Exclude,
		Chunking: chunk.ConfigFrom(a.Config.Chunking.TargetTokens, a.Config.Chunking.OverlapTokens),
		Semantic: f.semantic,
		ModelDir: modelDir,
	}

	_, err = eval.Report(cmd.Context(), a.Logger, cmd.OutOrStdout(), opts, baselinePath, f.save)

	return err
}
