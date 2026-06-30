package cli

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
)

// newWatchCmd builds the `watch <path> --collection <name>` command, which
// reconciles the collection against the directory tree and then watches it for
// live changes, reindexing modified files and removing vanished ones. Unlike
// serve, stdout is not a transport here, so progress is logged freely.
func newWatchCmd(state *rootState) *cobra.Command {
	var collection string
	cmd := &cobra.Command{
		Use:   "watch <path>",
		Short: "Watch a path and incrementally reindex changed and removed files",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWatch(cmd, state, args[0], collection)
		},
	}
	cmd.Flags().StringVar(&collection, "collection", "default", "collection name for the watched documents")

	return cmd
}

func runWatch(cmd *cobra.Command, state *rootState, path, collection string) error {
	return withStore(state, true, func(a *app.App) error {
		watcher, err := ingest.NewWatcher(a.DB, a.Logger, path, collection, ingest.WatchConfig{
			Include:         a.Config.Indexing.Include,
			Exclude:         a.Config.Indexing.Exclude,
			SecurityExclude: a.Config.SecurityExclude(),
			Chunking:        chunk.ConfigFrom(a.Config.Chunking.TargetTokens, a.Config.Chunking.OverlapTokens),
			StorageDir:      filepath.Dir(a.Layout.DB),
			MaxFileBytes:    a.Config.Indexing.MaxFileBytes,
		})
		if err != nil {
			return err
		}

		// Run until SIGINT/SIGTERM, then shut the watcher down cleanly.
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		a.Logger.Info("watch starting", "root", path, "collection", collection)
		if err := watcher.Run(ctx); err != nil {
			return fmt.Errorf("watch: run: %w", err)
		}
		a.Logger.Info("watch stopped", "root", path, "collection", collection)

		return nil
	})
}
