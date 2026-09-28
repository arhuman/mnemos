package cli

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/storage"
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
		if err := refuseOriginWatch(cmd, a, path); err != nil {
			return err
		}
		watcher, err := ingest.NewWatcher(a.DB, a.Logger, path, collection, ingest.WatchConfig{
			Include:         a.Config.Indexing.Include,
			Exclude:         a.Config.Indexing.Exclude,
			SecurityExclude: a.Config.SecurityExclude(),
			Chunking:        chunk.ConfigFrom(a.Config.Chunking.TargetTokens, a.Config.Chunking.OverlapTokens),
			StorageDir:      filepath.Dir(a.Layout.DB),
			URIBase:         a.TreeRoot(), // URIs are kb-relative even when watching a subtree
			MaxFileBytes:    a.Config.Indexing.MaxFileBytes,
			Encoding:        encodingRules(a.Config.EncodingRules()),
			ScanSecrets:     a.Config.Security.ExcludeSecrets,
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

// refuseOriginWatch rejects watching a registered origin's tree. The watcher is
// single-root per process and its walk does not descend symlinked directories,
// so it would appear to work while indexing nothing; refusing explicitly is
// better than a watch that silently observes an empty set (ADR-0013).
func refuseOriginWatch(cmd *cobra.Command, a *app.App, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("watch: resolve %q: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		resolved = abs
	}
	origins, err := storage.ListOrigins(cmd.Context(), a.DB)
	if err != nil {
		return err
	}
	for _, o := range origins {
		// An unrelatable path cannot be inside this origin, so skipping is correct
		// here (unlike registration, where the same question gates a write).
		rel, relErr := filepath.Rel(o.Path, resolved)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}

		return fmt.Errorf("watch: %q is inside registered origin %q, which watch does not support; run 'mnemos origin reindex %s' instead",
			path, o.Prefix, o.Prefix)
	}

	return nil
}
