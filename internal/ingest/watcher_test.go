package ingest

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/storage"
	"github.com/arhuman/mnemos/internal/testutil"
)

// watchDeadline is the upper bound a watcher integration test waits for an
// fsnotify-driven effect to land. It is intentionally generous: these tests
// assert that the event wiring eventually converges, not how fast it does, so a
// loaded machine (race detector, -count=N stress) never produces a false
// failure. The watcher's effectful logic is covered deterministically — without
// fsnotify or timers — in watcher_unit_test.go.
const watchDeadline = 10 * time.Second

// watchTestDB returns a fresh migrated database for watcher tests.
func watchTestDB(t *testing.T) *sql.DB { return testutil.NewDB(t) }

func writeNote(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

func testWatchConfig() WatchConfig {
	return WatchConfig{
		Include:  []string{"**/*.md"},
		Chunking: chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	}
}

// startWatcher launches a watcher in the background and returns a cancel func.
// It blocks until the watcher signals Ready (startup reconcile done and the live
// filesystem watch registered), so any file mutation a test performs afterwards
// is reliably observed — this is what makes the fsnotify-driven tests
// deterministic rather than racing the watcher's startup.
func startWatcher(t *testing.T, db *sql.DB, root, collection string, cfg WatchConfig) context.CancelFunc {
	t.Helper()
	w, err := NewWatcher(db, slog.New(slog.NewTextHandler(io.Discard, nil)), root, collection, cfg)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = w.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("watcher did not shut down in time")
		}
	})

	// Block until the watch is live. If Run dies during startup it returns
	// without closing Ready, so also wake on done (and a timeout backstop).
	select {
	case <-w.Ready():
	case <-done:
		t.Fatal("watcher exited before becoming ready")
	case <-time.After(watchDeadline):
		t.Fatal("watcher did not become ready in time")
	}

	return cancel
}

// countChunks returns the number of chunks for the document with the given uri.
func countChunks(t *testing.T, db *sql.DB, uri string) int {
	t.Helper()
	chunks, err := storage.GetChunksByDocURI(context.Background(), db, uri)
	require.NoError(t, err)

	return len(chunks)
}

// pollUntil retries cond until it returns true or the deadline elapses.
func pollUntil(t *testing.T, deadline time.Duration, cond func() bool) bool {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}

	return cond()
}

func TestWatcherIndexesCreatedFile(t *testing.T) {
	root := t.TempDir()
	db := watchTestDB(t)
	startWatcher(t, db, root, "c", testWatchConfig())

	writeNote(t, root, "note.md", "# Title\n\nfirst body\n")

	ok := pollUntil(t, watchDeadline, func() bool {
		return countChunks(t, db, "note.md") > 0
	})
	require.True(t, ok, "created file should be indexed")
}

func TestWatcherReindexesModifiedFile(t *testing.T) {
	root := t.TempDir()
	db := watchTestDB(t)
	startWatcher(t, db, root, "c", testWatchConfig())

	writeNote(t, root, "note.md", "# Title\n\noriginal sentinel\n")
	require.True(t, pollUntil(t, watchDeadline, func() bool {
		return countChunks(t, db, "note.md") > 0
	}), "file should be indexed first")

	writeNote(t, root, "note.md", "# Title\n\nupdated marker phrase\n")

	ok := pollUntil(t, watchDeadline, func() bool {
		chunks, err := storage.GetChunksByDocURI(context.Background(), db, "note.md")
		if err != nil || len(chunks) == 0 {
			return false
		}
		for _, c := range chunks {
			if strings.Contains(c.Content, "updated marker phrase") {
				return true
			}
		}

		return false
	})
	require.True(t, ok, "modified file content should be reflected in chunks")
}

func TestWatcherRemovesDeletedFile(t *testing.T) {
	root := t.TempDir()
	db := watchTestDB(t)
	startWatcher(t, db, root, "c", testWatchConfig())

	writeNote(t, root, "note.md", "# Title\n\nbody\n")
	require.True(t, pollUntil(t, watchDeadline, func() bool {
		return countChunks(t, db, "note.md") > 0
	}), "file should be indexed first")

	require.NoError(t, os.Remove(filepath.Join(root, "note.md")))

	ok := pollUntil(t, watchDeadline, func() bool {
		doc, err := storage.GetDocumentByURI(context.Background(), db, "note.md")

		return err == nil && doc == nil
	})
	require.True(t, ok, "deleted file's document should be removed")
}

func TestWatcherReconcileRemovesVanishedDocument(t *testing.T) {
	root := t.TempDir()
	db := watchTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Pre-seed a document whose backing file is absent on disk.
	p := New(db, logger)
	writeNote(t, root, "ghost.md", "# Ghost\n\nbody\n")
	_, _, err := p.IngestPath(context.Background(), filepath.Join(root, "ghost.md"), "ghost.md", "c", chunk.Config{TargetTokens: 700, OverlapTokens: 80})
	require.NoError(t, err)
	require.NoError(t, os.Remove(filepath.Join(root, "ghost.md")))

	doc, err := storage.GetDocumentByURI(context.Background(), db, "ghost.md")
	require.NoError(t, err)
	require.NotNil(t, doc, "document should exist before reconcile")

	// Starting the watcher runs reconcile, which must evict the vanished doc.
	startWatcher(t, db, root, "c", testWatchConfig())

	ok := pollUntil(t, watchDeadline, func() bool {
		d, err := storage.GetDocumentByURI(context.Background(), db, "ghost.md")

		return err == nil && d == nil
	})
	require.True(t, ok, "reconcile should remove the vanished document")
}

func TestDeleteByURICascades(t *testing.T) {
	root := t.TempDir()
	db := watchTestDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	writeNote(t, root, "doc.md", "# Doc\n\nbody [l](other.md)\n")
	_, chunks, err := New(db, logger).IngestPath(context.Background(),
		filepath.Join(root, "doc.md"), "doc.md", "c", chunk.Config{TargetTokens: 700, OverlapTokens: 80})
	require.NoError(t, err)
	require.Positive(t, chunks)
	require.Positive(t, countChunks(t, db, "doc.md"))

	require.NoError(t, storage.DeleteByURI(context.Background(), db, "doc.md"))

	doc, err := storage.GetDocumentByURI(context.Background(), db, "doc.md")
	require.NoError(t, err)
	require.Nil(t, doc, "document gone")
	require.Zero(t, countChunks(t, db, "doc.md"), "chunks cascade-deleted")

	var ftsRows int
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM chunks_fts`).Scan(&ftsRows))
	require.Zero(t, ftsRows, "FTS index emptied by chunks delete trigger")

	var linkRows int
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM links`).Scan(&linkRows))
	require.Zero(t, linkRows, "links cascade-deleted")

	// Deleting an absent uri is a no-op.
	require.NoError(t, storage.DeleteByURI(context.Background(), db, "missing.md"))
}

func TestDebouncerCoalescesRapidEvents(t *testing.T) {
	d := newDebouncer(50 * time.Millisecond)
	defer d.Stop()

	var calls atomic.Int64
	var wg sync.WaitGroup
	wg.Add(1)
	var once sync.Once

	for range 20 {
		d.trigger("same", func() {
			calls.Add(1)
			once.Do(wg.Done)
		})
		time.Sleep(2 * time.Millisecond)
	}

	wg.Wait()
	// Give a brief window for any erroneous extra callback to surface.
	time.Sleep(100 * time.Millisecond)
	require.Equal(t, int64(1), calls.Load(), "rapid events on one key fire one callback")
}

func TestDebouncerSeparateKeysFireIndependently(t *testing.T) {
	d := newDebouncer(30 * time.Millisecond)
	defer d.Stop()

	var calls atomic.Int64
	var wg sync.WaitGroup
	wg.Add(2)
	d.trigger("a", func() { calls.Add(1); wg.Done() })
	d.trigger("b", func() { calls.Add(1); wg.Done() })

	wg.Wait()
	require.Equal(t, int64(2), calls.Load())
}
