package app_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/app"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))

	return p
}

func TestNewLoggerLevel(t *testing.T) {
	ctx := context.Background()
	require.True(t, app.NewLogger(true).Enabled(ctx, slog.LevelDebug))
	require.False(t, app.NewLogger(false).Enabled(ctx, slog.LevelDebug))
	require.True(t, app.NewLogger(false).Enabled(ctx, slog.LevelInfo))
}

func TestLoadBuildsAppWithoutDB(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "mnemos.toml", "[storage]\npath = \"x.db\"\n")

	a, err := app.Load(path, false)
	require.NoError(t, err)
	require.NotNil(t, a.Config)
	require.NotNil(t, a.Logger)
	require.Nil(t, a.DB) // Load does not open storage.
	// A relative storage path is resolved once, against the --config directory
	// (the tree root), so it no longer silently follows the process cwd.
	require.Equal(t, filepath.Join(dir, "x.db"), a.Config.Storage.Path)
}

func TestLoadKeepsAbsoluteStoragePath(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "data", "mnemos.db")
	path := writeFile(t, dir, "mnemos.toml", "[storage]\npath = \""+abs+"\"\n")

	a, err := app.Load(path, false)
	require.NoError(t, err)
	require.Equal(t, abs, a.Config.Storage.Path)
}

func TestOpenStoreAndClose(t *testing.T) {
	a, err := app.Load("", false)
	require.NoError(t, err)
	a.Config.Storage.Path = filepath.Join(t.TempDir(), "mnemos.db")

	require.NoError(t, a.OpenStore(true))
	require.NotNil(t, a.DB)

	// Migrations ran: the documents table is queryable.
	var n int
	require.NoError(t, a.DB.QueryRow(`SELECT COUNT(*) FROM documents`).Scan(&n))
	require.Equal(t, 0, n)

	require.NoError(t, a.Close())
}

func TestTreeRoot(t *testing.T) {
	a, err := app.Load(filepath.Join("MEMORY", "mnemos.toml"), false)
	require.NoError(t, err)
	require.Equal(t, "MEMORY", a.TreeRoot())

	// A bare config name resolves the tree root to the current directory.
	a, err = app.Load("mnemos.toml", false)
	require.NoError(t, err)
	require.Equal(t, ".", a.TreeRoot())
}

func TestCloseWithoutDBIsNoOp(t *testing.T) {
	a, err := app.Load("", false)
	require.NoError(t, err)
	require.NoError(t, a.Close())
}

func TestOpenStoreErrorsOnUnusablePath(t *testing.T) {
	a, err := app.Load("", false)
	require.NoError(t, err)
	// Parent directory does not exist, so opening/creating the DB file fails.
	a.Config.Storage.Path = filepath.Join(t.TempDir(), "missing-subdir", "mnemos.db")
	require.Error(t, a.OpenStore(true))
	require.Nil(t, a.DB)
}

func TestOpenStoreRejectsMissingDBWhenCreateDisallowed(t *testing.T) {
	a, err := app.Load("", false)
	require.NoError(t, err)
	// Parent directory exists but the database file does not. With creation
	// disallowed (read commands), OpenStore must error instead of silently
	// creating an empty database, and must not create the file.
	path := filepath.Join(t.TempDir(), "mnemos.db")
	a.Config.Storage.Path = path

	require.Error(t, a.OpenStore(false))
	require.Nil(t, a.DB)
	require.NoFileExists(t, path)
}

func TestOpenStoreRejectsEmptyDBWhenCreateDisallowed(t *testing.T) {
	a, err := app.Load("", false)
	require.NoError(t, err)
	// A zero-byte file (e.g. left behind by the old silent-create behavior) is
	// treated as missing rather than opened as a valid empty database.
	path := filepath.Join(t.TempDir(), "mnemos.db")
	require.NoError(t, os.WriteFile(path, nil, 0o644))
	a.Config.Storage.Path = path

	require.Error(t, a.OpenStore(false))
	require.Nil(t, a.DB)
}
