package testutil_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/testutil"
)

func TestNewDBIsMigratedAndUsable(t *testing.T) {
	db := testutil.NewDB(t)
	var n int
	// The documents table exists (migrations ran) and is empty.
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM documents`).Scan(&n))
	require.Equal(t, 0, n)
}

func TestWriteFileCreatesParents(t *testing.T) {
	dir := t.TempDir()
	p := testutil.WriteFile(t, dir, "sub/nested/note.md", "hello")
	require.Equal(t, filepath.Join(dir, "sub", "nested", "note.md"), p)
	got, err := os.ReadFile(p)
	require.NoError(t, err)
	require.Equal(t, "hello", string(got))
}

func TestDiscardLogger(t *testing.T) {
	l := testutil.DiscardLogger()
	require.NotNil(t, l)
	require.True(t, l.Enabled(context.Background(), slog.LevelInfo))
	l.Info("this goes nowhere") // must not panic
}

func TestChdirSwitchesAndRestores(t *testing.T) {
	before, err := os.Getwd()
	require.NoError(t, err)

	target := t.TempDir()
	testutil.Chdir(t, target)

	cur, err := os.Getwd()
	require.NoError(t, err)
	// Resolve symlinks so macOS /var -> /private/var does not break equality.
	curResolved, err := filepath.EvalSymlinks(cur)
	require.NoError(t, err)
	targetResolved, err := filepath.EvalSymlinks(target)
	require.NoError(t, err)
	require.Equal(t, targetResolved, curResolved)

	// Restore now so a stale cwd cannot leak if cleanup ordering surprises us.
	require.NoError(t, os.Chdir(before))
}
