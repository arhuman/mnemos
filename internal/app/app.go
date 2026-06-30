// Package app wires together the mnemos workspace layout, configuration,
// logger, and storage into a single App value shared across CLI commands.
package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/storage"
	"github.com/arhuman/mnemos/internal/workspace"
)

// App holds the process-wide dependencies: the resolved workspace layout, parsed
// configuration, a structured logger, and an open database handle. Commands
// receive a built App rather than reaching for globals.
type App struct {
	Config *config.Config
	Layout workspace.Layout
	Logger *slog.Logger
	DB     *sql.DB
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

// LoadOptions carries the workspace-selection inputs from the root flags.
type LoadOptions struct {
	// ConfigPath is --config: an explicit mnemos.toml whose directory becomes the
	// MNEMOS_DIR. Empty means resolve by --mnemos-dir / $MNEMOS_DIR / project /
	// default (see workspace.Resolve).
	ConfigPath string
	// MnemosDir is --mnemos-dir: an explicit MNEMOS_DIR.
	MnemosDir string
	Verbose   bool
}

// Load resolves the workspace layout (the single MNEMOS_DIR and every path
// derived from it) and loads the mnemos.toml found there. It does NOT open the
// database; call OpenStore once the layout is known and the directory exists.
//
// Every location — knowledge base, capture, index database, models — is a fixed
// subpath of the resolved MNEMOS_DIR, so the database, writes, and the URI
// namespace all anchor to one place regardless of the process working directory
// (which an MCP client does not guarantee).
func Load(opts LoadOptions) (*App, error) {
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	layout, err := workspace.Resolve(workspace.Options{
		ExplicitDir: opts.MnemosDir,
		ConfigPath:  opts.ConfigPath,
		Env:         os.Getenv,
		Home:        home,
		Cwd:         cwd,
	})
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load(layout.Config, fileExists)
	if err != nil {
		return nil, err
	}

	return &App{
		Config: cfg,
		Layout: layout,
		Logger: NewLogger(opts.Verbose),
	}, nil
}

// TreeRoot returns the knowledge base root (MNEMOS_DIR/kb): the directory
// caller-supplied paths for capture, forget, and move are resolved relative to
// and confined within, and the base of every citation URI.
func (a *App) TreeRoot() string {
	return a.Layout.KB
}

// OpenStore opens the workspace index database and runs migrations, storing the
// handle on the App. Callers are responsible for ensuring the parent directory
// exists.
//
// allowCreate gates database creation. Write/populate commands (init, ingest,
// add, watch, okfy) pass true and may create a fresh database. Read commands
// (serve, search, ls, status, task, reindex, forget, mv) pass false: if the
// database is absent or empty, OpenStore returns an actionable error rather than
// letting the SQLite driver silently create an empty database — the failure mode
// that made a misconfigured MCP server return no results while appearing to work.
func (a *App) OpenStore(allowCreate bool) error {
	path := a.Layout.DB

	if !allowCreate {
		switch info, statErr := os.Stat(path); {
		case errors.Is(statErr, fs.ErrNotExist):
			return fmt.Errorf("app: no mnemos database at %q (MNEMOS_DIR %q, %s); run \"mnemos init\" or \"mnemos add\" first", path, a.Layout.MnemosDir, a.Layout.Source)
		case statErr != nil:
			return fmt.Errorf("app: stat database %q: %w", path, statErr)
		case info.Size() == 0:
			return fmt.Errorf("app: mnemos database at %q is empty (0 bytes); run \"mnemos add\" first, or remove the stale file", path)
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
