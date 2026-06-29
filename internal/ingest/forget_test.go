package ingest

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/storage"
)

func TestForgetPathRemovesFileAndIndex(t *testing.T) {
	db := moveTestDB(t)
	root := t.TempDir()
	logger := moveTestLogger()

	abs := filepath.Join(root, "tech", "note.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte("# Note\n\nbody.\n"), 0o644))
	_, _, err := File(context.Background(), db, logger, abs, "tech/note.md", "tech", moveTestCfg())
	require.NoError(t, err)

	deleted, err := ForgetPath(context.Background(), db, logger, abs, "tech/note.md")
	require.NoError(t, err)
	require.True(t, deleted)
	require.NoFileExists(t, abs)

	_, ok, err := storage.CollectionByURI(context.Background(), db, "tech/note.md")
	require.NoError(t, err)
	require.False(t, ok, "the document must be de-indexed")
}

func TestForgetPathIdempotentOnMissingFile(t *testing.T) {
	db := moveTestDB(t)
	logger := moveTestLogger()

	// Neither a file on disk nor an index row: forgetting is a no-op that reports
	// deleted=false rather than erroring.
	deleted, err := ForgetPath(context.Background(), db, logger, filepath.Join(t.TempDir(), "nope.md"), "nope.md")
	require.NoError(t, err)
	require.False(t, deleted)
}

func TestForgetPathLeavesFileWhenDeindexFails(t *testing.T) {
	db := moveTestDB(t)
	root := t.TempDir()
	logger := moveTestLogger()

	abs := filepath.Join(root, "keep.md")
	require.NoError(t, os.WriteFile(abs, []byte("# Keep\n\nbody.\n"), 0o644))

	// Closing the DB forces the de-index step to fail. Because forget is DB-first,
	// the disk file must survive: a failed forget never removes the file while
	// leaving the index inconsistent. This is the crash-coherence guarantee both
	// the CLI and MCP surfaces now share.
	require.NoError(t, db.Close())

	_, err := ForgetPath(context.Background(), db, logger, abs, "keep.md")
	require.Error(t, err)
	require.FileExists(t, abs, "a failed de-index must leave the file on disk")
}
