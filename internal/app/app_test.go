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

func TestLoadBuildsAppWithDerivedLayout(t *testing.T) {
	dir := t.TempDir()

	a, err := app.Load(app.LoadOptions{MnemosDir: dir})
	require.NoError(t, err)
	require.NotNil(t, a.Config)
	require.NotNil(t, a.Logger)
	require.Nil(t, a.DB) // Load does not open storage.
	require.Equal(t, dir, a.Layout.MnemosDir)
	require.Equal(t, filepath.Join(dir, "kb"), a.Layout.KB)
	require.Equal(t, filepath.Join(dir, "kb", "capture"), a.Layout.Capture)
	require.Equal(t, filepath.Join(dir, "state", "index.db"), a.Layout.DB)
}

func TestLoadConfigPathSetsMnemosDir(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "custom.toml", "[search]\ndefault_limit = 7\n")

	a, err := app.Load(app.LoadOptions{ConfigPath: path})
	require.NoError(t, err)
	require.Equal(t, dir, a.Layout.MnemosDir)
	require.Equal(t, path, a.Layout.Config)
	require.Equal(t, 7, a.Config.Search.DefaultLimit)
}

func TestOpenStoreAndClose(t *testing.T) {
	a, err := app.Load(app.LoadOptions{MnemosDir: t.TempDir()})
	require.NoError(t, err)
	a.Layout.DB = filepath.Join(t.TempDir(), "mnemos.db")

	require.NoError(t, a.OpenStore(true))
	require.NotNil(t, a.DB)

	// Migrations ran: the documents table is queryable.
	var n int
	require.NoError(t, a.DB.QueryRow(`SELECT COUNT(*) FROM documents`).Scan(&n))
	require.Equal(t, 0, n)

	require.NoError(t, a.Close())
}

func TestTreeRootIsKB(t *testing.T) {
	dir := t.TempDir()

	a, err := app.Load(app.LoadOptions{MnemosDir: dir})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "kb"), a.TreeRoot())
}

func TestCloseWithoutDBIsNoOp(t *testing.T) {
	a, err := app.Load(app.LoadOptions{MnemosDir: t.TempDir()})
	require.NoError(t, err)
	require.NoError(t, a.Close())
}

func TestOpenStoreErrorsOnUnusablePath(t *testing.T) {
	a, err := app.Load(app.LoadOptions{MnemosDir: t.TempDir()})
	require.NoError(t, err)
	// Parent directory does not exist, so opening/creating the DB file fails.
	a.Layout.DB = filepath.Join(t.TempDir(), "missing-subdir", "mnemos.db")
	require.Error(t, a.OpenStore(true))
	require.Nil(t, a.DB)
}

func TestOpenStoreRejectsMissingDBWhenCreateDisallowed(t *testing.T) {
	a, err := app.Load(app.LoadOptions{MnemosDir: t.TempDir()})
	require.NoError(t, err)
	// Parent directory exists but the database file does not. With creation
	// disallowed (read commands), OpenStore must error instead of silently
	// creating an empty database, and must not create the file.
	path := filepath.Join(t.TempDir(), "mnemos.db")
	a.Layout.DB = path

	require.Error(t, a.OpenStore(false))
	require.Nil(t, a.DB)
	require.NoFileExists(t, path)
}

func TestOpenStoreRejectsEmptyDBWhenCreateDisallowed(t *testing.T) {
	a, err := app.Load(app.LoadOptions{MnemosDir: t.TempDir()})
	require.NoError(t, err)
	// A zero-byte file (e.g. left behind by an old silent-create) is treated as
	// missing rather than opened as a valid empty database.
	path := filepath.Join(t.TempDir(), "mnemos.db")
	require.NoError(t, os.WriteFile(path, nil, 0o644))
	a.Layout.DB = path

	require.Error(t, a.OpenStore(false))
	require.Nil(t, a.DB)
}
