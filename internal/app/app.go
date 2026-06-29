// Package app wires together the mnemos configuration, logger, and storage
// into a single App value shared across CLI commands.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/storage"
)

// App holds the process-wide dependencies: parsed configuration, a structured
// logger, and an open database handle. Commands receive a built App rather than
// reaching for globals.
type App struct {
	Config *config.Config
	Logger *slog.Logger
	DB     *sql.DB
	// treeRoot is the writable tree root for capture/forget/move, resolved at
	// load time from the config source (see config.Resolve and TreeRoot).
	treeRoot string
}

// NewLogger returns a slog text logger writing to stderr. When verbose is set
// the level is Debug, otherwise Info.
func NewLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})

	return slog.New(handler)
}

// Load builds an App: it resolves and layers configuration (defaults, then
// ~/.mnemos.toml and ./.mnemos.toml, or the explicit configPath when set) and
// constructs the logger. It does NOT open the database; call OpenStore once the
// storage path is known and the directory exists.
//
// A relative [storage].path is resolved here, once, against the same tree root
// as capture/forget/move (the --config directory, or the current directory in
// auto-discovery mode) so the stored path is absolute and every command, log
// line, and error message shares a single source of truth. Without this, a
// relative path silently followed the process cwd — which, for an MCP stdio
// server launched by a client, is not the project directory.
func Load(configPath string, verbose bool) (*App, error) {
	home, _ := os.UserHomeDir()
	paths, treeRoot := config.Resolve(configPath, home)

	cfg, err := config.Load(paths, fileExists)
	if err != nil {
		return nil, err
	}

	if !filepath.IsAbs(cfg.Storage.Path) {
		cfg.Storage.Path = filepath.Clean(filepath.Join(treeRoot, cfg.Storage.Path))
	}

	return &App{
		Config:   cfg,
		Logger:   NewLogger(verbose),
		treeRoot: treeRoot,
	}, nil
}

// TreeRoot returns the writable OKF tree root: the directory caller-supplied
// paths for capture, forget, and move are resolved relative to (and confined
// within). With an explicit --config it is that file's directory; in
// auto-discovery mode it is the current working directory.
func (a *App) TreeRoot() string {
	return a.treeRoot
}

// OpenStore opens the configured SQLite database and runs migrations, storing
// the handle on the App. Callers are responsible for ensuring the parent
// directory exists.
//
// allowCreate gates database creation. Write/populate commands (init, ingest,
// watch, okfy) pass true and may create a fresh database. Read commands (serve,
// search, ls, status, task, reindex, forget, mv) pass false: if the database is
// absent or empty, OpenStore returns an actionable error rather than letting the
// SQLite driver silently create an empty database — the failure mode that made a
// misconfigured MCP server return no results while appearing to work.
func (a *App) OpenStore(allowCreate bool) error {
	path := a.Config.Storage.Path

	if !allowCreate {
		switch info, statErr := os.Stat(path); {
		case errors.Is(statErr, fs.ErrNotExist):
			cwd, _ := os.Getwd()

			return fmt.Errorf("app: no mnemos database at %q (tree root %q, cwd %q); run \"mnemos init\" or \"mnemos ingest\" first, or point --config / [storage].path at the right file", path, a.treeRoot, cwd)
		case statErr != nil:
			return fmt.Errorf("app: stat database %q: %w", path, statErr)
		case info.Size() == 0:
			return fmt.Errorf("app: mnemos database at %q is empty (0 bytes); run \"mnemos ingest\" first, or remove the stale file", path)
		}
	}

	db, err := storage.Open(context.Background(), path)
	if err != nil {
		return err
	}
	if err := storage.Migrate(db); err != nil {
		_ = db.Close()

		return err
	}
	a.DB = db
	a.Logger.Info("storage opened", "path", path)

	return nil
}

// Close releases the database handle if one is open.
func (a *App) Close() error {
	if a.DB == nil {
		return nil
	}
	if err := a.DB.Close(); err != nil {
		return fmt.Errorf("app: close db: %w", err)
	}

	return nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && !info.IsDir()
}
