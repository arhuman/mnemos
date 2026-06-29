// Package testutil provides shared, dependency-light helpers for tests across
// the mnemos packages: a migrated temp database, file creation, a silent
// logger, and a working-directory switch. It imports only internal/storage, so
// white-box (internal) test packages such as internal/ingest can use it without
// creating an import cycle. Corpus building (which needs internal/ingest) is
// deliberately left to the packages that need it, to keep this package
// cycle-free.
package testutil

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/storage"
)

// NewDB opens a fresh migrated SQLite database under a per-test temp directory
// and registers its close on cleanup. It is the canonical replacement for the
// per-package "open temp migrated db" helpers.
func NewDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "mnemos.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))

	return db
}

// WriteFile writes content to dir/rel, creating parent directories, and returns
// the absolute path written.
func WriteFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o600))

	return p
}

// DiscardLogger returns a slog logger that writes nowhere, keeping test output
// clean while still exercising logging code paths.
func DiscardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Chdir switches into dir for the duration of the test, restoring the previous
// working directory on cleanup.
func Chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(prev) })
}
