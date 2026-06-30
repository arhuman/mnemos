package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/security"
)

// newIngestCmd builds the `ingest <path> --collection <name>` command, which
// scans a path, parses and chunks the matched files, and writes
// documents/chunks/links/FTS into the store.
func newIngestCmd(state *rootState) *cobra.Command {
	var collection string
	cmd := &cobra.Command{
		Use:   "ingest <path>",
		Short: "Scan a path and index its documents into a collection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIngest(cmd, state, args[0], collection)
		},
	}
	cmd.Flags().StringVar(&collection, "collection", "default", "collection name for the ingested documents")

	return cmd
}

func runIngest(cmd *cobra.Command, state *rootState, path, collection string) error {
	return withStore(state, true, func(a *app.App) error {
		// Confine the scan root to the tree root: ingesting a path outside it
		// would mint URIs whose files live where read/ls/move (all tree-root
		// anchored) cannot resolve them. okfy already confines its source the
		// same way; this closes the gap for bulk ingest.
		scanRoot, err := security.ConfineDir(a.TreeRoot(), path)
		if err != nil {
			return fmt.Errorf("ingest: %w; copy the content into the tree first, then ingest it from there", err)
		}

		opts := ingest.Options{
			Root:       scanRoot,
			Collection: collection,
			Rules: ingest.Rules{
				Include:         a.Config.Indexing.Include,
				Exclude:         a.Config.Indexing.Exclude,
				SecurityExclude: a.Config.SecurityExclude(),
			},
			Chunking: chunk.ConfigFrom(a.Config.Chunking.TargetTokens, a.Config.Chunking.OverlapTokens),
		}

		summary, err := ingest.New(a.DB, a.Logger, ingest.WithMaxFileBytes(a.Config.Indexing.MaxFileBytes)).Run(cmd.Context(), opts)
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()
		_, _ = fmt.Fprintf(out, "collection:      %s\n", collection)
		_, _ = fmt.Fprintf(out, "files scanned:   %d\n", summary.FilesScanned)
		_, _ = fmt.Fprintf(out, "files ingested:  %d\n", summary.FilesIngested)
		_, _ = fmt.Fprintf(out, "files skipped:   %d\n", summary.FilesSkipped)
		_, _ = fmt.Fprintf(out, "chunks written:  %d\n", summary.ChunksWritten)

		return nil
	})
}
