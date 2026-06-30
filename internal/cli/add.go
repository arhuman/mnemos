package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/security"
)

// newAddCmd builds `add <source> [--into <subpath>] [--mode copy|link]`, the
// managed-store entry point: it brings external content INTO the knowledge base
// (by copy or symlink) and indexes it, so every URI resolves under the kb root.
func newAddCmd(state *rootState) *cobra.Command {
	var into, collection, mode string
	cmd := &cobra.Command{
		Use:   "add <source>",
		Short: "Copy (or link) external content into the knowledge base and index it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(cmd, state, args[0], into, collection, mode)
		},
	}
	cmd.Flags().StringVar(&into, "into", "", "destination subpath within the kb (default: the source's base name)")
	cmd.Flags().StringVar(&collection, "collection", "default", "collection name for the added documents")
	cmd.Flags().StringVar(&mode, "mode", "copy", "how to bring content in: copy (snapshot) or link (symlink)")

	return cmd
}

func runAdd(cmd *cobra.Command, state *rootState, src, into, collection, mode string) error {
	return withStore(state, true, func(a *app.App) error {
		srcAbs, err := filepath.Abs(src)
		if err != nil {
			return fmt.Errorf("add: %w", err)
		}
		srcInfo, err := os.Stat(srcAbs)
		if err != nil {
			return fmt.Errorf("add: source %q: %w", src, err)
		}

		rel := into
		if rel == "" {
			rel = filepath.Base(srcAbs)
		}
		// Confine the destination to the kb so add can never write outside it.
		dest, err := security.ConfineDir(a.TreeRoot(), rel)
		if err != nil {
			return fmt.Errorf("add: --into %q: %w", rel, err)
		}

		switch mode {
		case "", "copy":
			if err = copyPath(srcAbs, dest, srcInfo); err != nil {
				return fmt.Errorf("add: copy: %w", err)
			}
		case "link":
			if err = os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
				return fmt.Errorf("add: link: %w", err)
			}
			if err = os.Symlink(srcAbs, dest); err != nil {
				return fmt.Errorf("add: link: %w", err)
			}
		default:
			return fmt.Errorf("add: unknown --mode %q (want copy or link)", mode)
		}

		opts := ingest.Options{
			Root:       dest,
			URIBase:    a.TreeRoot(), // URIs are kb-relative regardless of --into
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
		_, _ = fmt.Fprintf(out, "added:           %s (%s)\n", rel, mode)
		_, _ = fmt.Fprintf(out, "collection:      %s\n", collection)
		_, _ = fmt.Fprintf(out, "files ingested:  %d\n", summary.FilesIngested)
		_, _ = fmt.Fprintf(out, "chunks written:  %d\n", summary.ChunksWritten)

		return nil
	})
}

// copyPath copies src to dest. When src is a directory the whole subtree is
// copied; parent directories are created as needed.
func copyPath(src, dest string, info os.FileInfo) error {
	if !info.IsDir() {
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			return err
		}

		return copyFile(src, dest)
	}

	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}

		return copyFile(path, target)
	})
}

func copyFile(src, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	in, err := os.Open(src) //nolint:gosec // add deliberately copies a user-specified source path into the kb
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm()) //nolint:gosec // dest is confined to the kb by ConfineDir
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()

		return err
	}

	return out.Close()
}
