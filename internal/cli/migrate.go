package cli

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/workspace"
)

// newMigrateCmd builds `migrate --from <old> [--to <dir>] [--move]`, which moves
// a pre-MNEMOS_DIR workspace into the new layout: it relocates the old tree-root
// content under <dir>/kb (old capture under kb/capture), then reindexes. A
// document's own collection: frontmatter is preserved by the re-index.
func newMigrateCmd(state *rootState) *cobra.Command {
	var from, to string
	var move bool
	cmd := &cobra.Command{
		Use:   "migrate --from <old-root-or-config>",
		Short: "Move a pre-MNEMOS_DIR workspace into the kb/ layout and reindex",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMigrate(cmd, state, from, to, move)
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "old tree root, or old config file whose directory is the tree root (required)")
	cmd.Flags().StringVar(&to, "to", "", "target MNEMOS_DIR (default: ~/.mnemos)")
	cmd.Flags().BoolVar(&move, "move", false, "move content instead of copying (the source is left intact by default)")

	return cmd
}

func runMigrate(cmd *cobra.Command, state *rootState, from, to string, move bool) error {
	if from == "" {
		return errors.New("migrate: --from is required")
	}

	fromAbs, err := filepath.Abs(from)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	info, err := os.Stat(fromAbs)
	if err != nil {
		return fmt.Errorf("migrate: --from %q: %w", from, err)
	}
	// --from may name the old tree root or its old config file.
	var oldConfigBase string
	oldRoot := fromAbs
	if !info.IsDir() {
		oldRoot = filepath.Dir(fromAbs)
		oldConfigBase = filepath.Base(fromAbs)
	}

	target := to
	if target == "" {
		home, herr := os.UserHomeDir()
		if herr != nil || home == "" {
			return errors.New("migrate: --to is required (no home directory for the default ~/.mnemos)")
		}
		target = filepath.Join(home, workspace.DirName)
	}
	layout := workspace.New(target)
	if filepath.Clean(layout.KB) == filepath.Clean(oldRoot) {
		return fmt.Errorf("migrate: target kb %q equals the source root; choose a different --to", layout.KB)
	}

	out := cmd.OutOrStdout()
	logger := app.NewLogger(state.flags.verbose)

	for _, dir := range []string{layout.Capture, filepath.Dir(layout.DB), layout.Models} {
		if err = os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("migrate: create %q: %w", dir, err)
		}
	}
	if _, statErr := os.Stat(layout.Config); errors.Is(statErr, os.ErrNotExist) {
		if err = os.WriteFile(layout.Config, config.DefaultTOML(), 0o600); err != nil {
			return fmt.Errorf("migrate: write config %q: %w", layout.Config, err)
		}
	}

	moved, err := relocateContent(oldRoot, oldConfigBase, layout, move)
	if err != nil {
		return err
	}
	verb := "copied"
	if move {
		verb = "moved"
	}
	_, _ = fmt.Fprintf(out, "%s %d top-level entries into %s\n", verb, moved, layout.KB)

	// Reindex the relocated kb. Each document's collection: frontmatter is
	// authoritative, so original collections survive; files without one fall to
	// "default".
	summary, err := reindexKB(cmd, layout, logger)
	if err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "reindexed:       %d files, %d chunks\n", summary.FilesIngested, summary.ChunksWritten)
	_, _ = fmt.Fprintf(out, "migrated to:     %s (run `mnemos status --mnemos-dir %s`)\n", layout.MnemosDir, layout.MnemosDir)

	return nil
}

// relocateContent copies (or moves) each top-level entry of oldRoot into the new
// kb, and the old .mnemos/capture notes into kb/capture. The old internal
// .mnemos directory and the old config file are not relocated. It returns the
// number of top-level entries relocated.
func relocateContent(oldRoot, oldConfigBase string, layout workspace.Layout, move bool) (int, error) {
	entries, err := os.ReadDir(oldRoot)
	if err != nil {
		return 0, fmt.Errorf("migrate: read %q: %w", oldRoot, err)
	}

	count := 0
	for _, e := range entries {
		name := e.Name()
		if name == workspace.DirName || name == oldConfigBase {
			continue
		}
		src := filepath.Join(oldRoot, name)
		dst := filepath.Join(layout.KB, name)
		if err := transfer(src, dst, move); err != nil {
			return count, fmt.Errorf("migrate: relocate %q: %w", name, err)
		}
		count++
	}

	// Bring the old capture notes (under oldRoot/.mnemos/capture) into kb/capture.
	oldCapture := filepath.Join(oldRoot, workspace.DirName, "capture")
	if isDirPath(oldCapture) {
		capEntries, rerr := os.ReadDir(oldCapture)
		if rerr != nil {
			return count, fmt.Errorf("migrate: read old capture: %w", rerr)
		}
		for _, e := range capEntries {
			if err := transfer(filepath.Join(oldCapture, e.Name()), filepath.Join(layout.Capture, e.Name()), move); err != nil {
				return count, fmt.Errorf("migrate: relocate capture %q: %w", e.Name(), err)
			}
		}
	}

	return count, nil
}

// reindexKB opens the workspace store and indexes its kb, returning the ingest
// summary. Collection: frontmatter wins, so original collections are preserved.
func reindexKB(cmd *cobra.Command, layout workspace.Layout, logger *slog.Logger) (ingest.Summary, error) {
	cfg, err := config.Load(layout.Config, func(p string) bool {
		fi, e := os.Stat(p)

		return e == nil && !fi.IsDir()
	})
	if err != nil {
		return ingest.Summary{}, err
	}
	a := &app.App{Config: cfg, Layout: layout, Logger: logger}
	if err := a.OpenStore(true); err != nil {
		return ingest.Summary{}, err
	}
	defer func() { _ = a.Close() }()

	return ingest.New(a.DB, a.Logger, ingest.WithMaxFileBytes(cfg.Indexing.MaxFileBytes)).Run(cmd.Context(), ingest.Options{
		Root:       layout.KB,
		Collection: "default",
		Rules: ingest.Rules{
			Include:         cfg.Indexing.Include,
			Exclude:         cfg.Indexing.Exclude,
			SecurityExclude: cfg.SecurityExclude(),
		},
		Chunking: chunk.ConfigFrom(cfg.Chunking.TargetTokens, cfg.Chunking.OverlapTokens),
	})
}

// transfer copies src to dst, or moves it when move is set. Move falls back to
// copy+remove when os.Rename fails (e.g. across filesystems).
func transfer(src, dst string, move bool) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if move {
		if err := os.Rename(src, dst); err == nil {
			return nil
		}
		// Cross-device or other rename failure: fall back to copy then remove.
		if err := copyPath(src, dst, info); err != nil {
			return err
		}

		return os.RemoveAll(src)
	}

	return copyPath(src, dst, info)
}

func isDirPath(p string) bool {
	info, err := os.Stat(p)

	return err == nil && info.IsDir()
}
