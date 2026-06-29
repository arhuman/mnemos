package ingest

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/storage"
	"github.com/arhuman/mnemos/internal/testutil"
)

// newTestWatcher builds a Watcher over a temp root + db, with the storage dir
// set so the ignore logic can be exercised. Returns the watcher and its root.
// These tests call the watcher's effectful methods directly — no fsnotify, no
// debounce timers, no sleeps — so they are deterministic (the de-flaked path).
func newTestWatcher(t *testing.T, db *sql.DB) (*Watcher, string) {
	t.Helper()
	root := t.TempDir()
	cfg := testWatchConfig()
	cfg.StorageDir = ".mnemos"
	w, err := NewWatcher(db, testutil.DiscardLogger(), root, "c", cfg)
	require.NoError(t, err)

	return w, root
}

func TestReindexPathIndexesAndIsIdempotent(t *testing.T) {
	db := watchTestDB(t)
	w, root := newTestWatcher(t, db)
	ctx := context.Background()

	writeNote(t, root, "note.md", "# Title\n\nbody text\n")
	abs := filepath.Join(root, "note.md")

	w.reindexPath(ctx, abs, "note.md")
	require.Positive(t, countChunks(t, db, "note.md"))

	// Re-running on the unchanged file is a hash-skip no-op (still indexed).
	w.reindexPath(ctx, abs, "note.md")
	require.Positive(t, countChunks(t, db, "note.md"))

	// A path that vanished before the debounced callback is a silent no-op.
	w.reindexPath(ctx, filepath.Join(root, "gone.md"), "gone.md")
	doc, err := storage.GetDocumentByURI(context.Background(), db, "gone.md")
	require.NoError(t, err)
	require.Nil(t, doc)
}

func TestDeletePathEvicts(t *testing.T) {
	db := watchTestDB(t)
	w, root := newTestWatcher(t, db)
	ctx := context.Background()

	writeNote(t, root, "note.md", "# Title\n\nbody\n")
	w.reindexPath(ctx, filepath.Join(root, "note.md"), "note.md")
	require.Positive(t, countChunks(t, db, "note.md"))

	w.deletePath(context.Background(), "note.md")
	doc, err := storage.GetDocumentByURI(context.Background(), db, "note.md")
	require.NoError(t, err)
	require.Nil(t, doc)

	// Deleting an unknown uri is a no-op (no panic, no error surfaced).
	w.deletePath(context.Background(), "never.md")
}

func TestWatcherIgnoreLogic(t *testing.T) {
	db := watchTestDB(t)
	w, root := newTestWatcher(t, db)

	t.Run("storage dir itself", func(t *testing.T) {
		require.True(t, w.isIgnored(filepath.Join(root, ".mnemos")))
		require.True(t, w.isStorageDir(filepath.Join(root, ".mnemos")))
	})
	t.Run("file under storage dir", func(t *testing.T) {
		require.True(t, w.isIgnored(filepath.Join(root, ".mnemos", "mnemos.db")))
	})
	t.Run("database file by basename anywhere", func(t *testing.T) {
		require.True(t, w.isIgnored(filepath.Join(root, "sub", "mnemos.db-wal")))
	})
	t.Run("ordinary note not ignored", func(t *testing.T) {
		require.False(t, w.isIgnored(filepath.Join(root, "note.md")))
	})
}

func TestAbsStorageDir(t *testing.T) {
	db := watchTestDB(t)

	t.Run("relative resolves against root", func(t *testing.T) {
		w, root := newTestWatcher(t, db) // StorageDir = ".mnemos"
		require.Equal(t, filepath.Join(root, ".mnemos"), w.absStorageDir())
	})

	t.Run("absolute is cleaned as-is", func(t *testing.T) {
		root := t.TempDir()
		cfg := testWatchConfig()
		cfg.StorageDir = "/var/lib/mnemos/../mnemos"
		w, err := NewWatcher(db, testutil.DiscardLogger(), root, "c", cfg)
		require.NoError(t, err)
		require.Equal(t, filepath.Clean("/var/lib/mnemos/../mnemos"), w.absStorageDir())
	})

	t.Run("empty storage dir yields empty", func(t *testing.T) {
		root := t.TempDir()
		w, err := NewWatcher(db, testutil.DiscardLogger(), root, "c", testWatchConfig())
		require.NoError(t, err)
		require.Empty(t, w.absStorageDir())
	})
}
