package ingest

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/storage"
	"github.com/arhuman/mnemos/internal/testutil"
)

func moveTestCfg() chunk.Config { return chunk.Config{TargetTokens: 700, OverlapTokens: 80} }

func moveTestLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func moveTestDB(t *testing.T) *sql.DB { return testutil.NewDB(t) }

func TestMovePathFile(t *testing.T) {
	db := moveTestDB(t)
	root := t.TempDir()
	logger := moveTestLogger()

	abs := filepath.Join(root, "perso", "note.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte("# Note\n\nMovable.\n"), 0o644))
	_, _, err := File(context.Background(), db, logger, abs, "perso/note.md", "perso", moveTestCfg())
	require.NoError(t, err)

	to := filepath.Join(root, "tech", "note.md")
	res, err := MovePath(context.Background(), db, logger, abs, to, "perso/note.md", "tech/note.md", moveTestCfg())
	require.NoError(t, err)
	require.False(t, res.IsDir)
	require.Len(t, res.Entries, 1)
	require.Equal(t, "tech/note.md", res.Entries[0].To)
	require.NotEmpty(t, res.Entries[0].DocumentID)

	require.NoFileExists(t, abs)
	require.FileExists(t, to)

	// Collection preserved; old uri de-indexed, new uri indexed.
	coll, ok, err := storage.CollectionByURI(context.Background(), db, "tech/note.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "perso", coll)
	_, ok, err = storage.CollectionByURI(context.Background(), db, "perso/note.md")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestMovePathFileNotIndexedUsesDefault(t *testing.T) {
	db := moveTestDB(t)
	root := t.TempDir()
	logger := moveTestLogger()

	abs := filepath.Join(root, "loose.md")
	require.NoError(t, os.WriteFile(abs, []byte("# Loose\n\nbody.\n"), 0o644))

	to := filepath.Join(root, "kept.md")
	res, err := MovePath(context.Background(), db, logger, abs, to, "loose.md", "kept.md", moveTestCfg())
	require.NoError(t, err)
	require.Len(t, res.Entries, 1)
	require.FileExists(t, to)

	coll, ok, err := storage.CollectionByURI(context.Background(), db, "kept.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "default", coll)
}

func TestMovePathDirectory(t *testing.T) {
	db := moveTestDB(t)
	root := t.TempDir()
	logger := moveTestLogger()

	one := filepath.Join(root, "adr", "one.md")
	two := filepath.Join(root, "adr", "sub", "two.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(two), 0o755))
	require.NoError(t, os.WriteFile(one, []byte("# One\n\nbody.\n"), 0o644))
	require.NoError(t, os.WriteFile(two, []byte("# Two\n\nbody.\n"), 0o644))
	_, _, err := File(context.Background(), db, logger, one, "adr/one.md", "arch", moveTestCfg())
	require.NoError(t, err)
	_, _, err = File(context.Background(), db, logger, two, "adr/sub/two.md", "arch", moveTestCfg())
	require.NoError(t, err)

	res, err := MovePath(context.Background(), db, logger,
		filepath.Join(root, "adr"), filepath.Join(root, "archive"), "adr", "archive", moveTestCfg())
	require.NoError(t, err)
	require.True(t, res.IsDir)
	require.Len(t, res.Entries, 2)

	require.NoDirExists(t, filepath.Join(root, "adr"))
	require.FileExists(t, filepath.Join(root, "archive", "one.md"))
	require.FileExists(t, filepath.Join(root, "archive", "sub", "two.md"))

	for _, uri := range []string{"archive/one.md", "archive/sub/two.md"} {
		coll, ok, cerr := storage.CollectionByURI(context.Background(), db, uri)
		require.NoError(t, cerr)
		require.True(t, ok)
		require.Equal(t, "arch", coll)
	}
	_, ok, err := storage.CollectionByURI(context.Background(), db, "adr/one.md")
	require.NoError(t, err)
	require.False(t, ok)
}

// TestMovePathRenameFailureKeepsIndex verifies the rename-first ordering: when
// os.Rename fails (here the destination is an existing non-empty directory), the
// index is left untouched — the document is still searchable under its original
// uri and the source file is still on disk — rather than de-indexed into a gap.
func TestMovePathRenameFailureKeepsIndex(t *testing.T) {
	db := moveTestDB(t)
	root := t.TempDir()
	logger := moveTestLogger()

	abs := filepath.Join(root, "note.md")
	require.NoError(t, os.WriteFile(abs, []byte("# Note\n\nbody.\n"), 0o644))
	_, _, err := File(context.Background(), db, logger, abs, "note.md", "perso", moveTestCfg())
	require.NoError(t, err)

	// Make the destination an existing non-empty directory so os.Rename fails.
	to := filepath.Join(root, "dest.md")
	require.NoError(t, os.MkdirAll(to, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(to, "occupied"), []byte("x"), 0o644))

	_, err = MovePath(context.Background(), db, logger, abs, to, "note.md", "dest.md", moveTestCfg())
	require.Error(t, err)

	// Index untouched: the original uri is still indexed and the file is on disk.
	require.FileExists(t, abs)
	coll, ok, err := storage.CollectionByURI(context.Background(), db, "note.md")
	require.NoError(t, err)
	require.True(t, ok, "old uri must remain indexed after a failed rename")
	require.Equal(t, "perso", coll)
}

func TestMovePathMissingSource(t *testing.T) {
	db := moveTestDB(t)
	logger := moveTestLogger()
	root := t.TempDir()

	_, err := MovePath(context.Background(), db, logger,
		filepath.Join(root, "ghost.md"), filepath.Join(root, "x.md"), "ghost.md", "x.md", moveTestCfg())
	require.Error(t, err)
}
