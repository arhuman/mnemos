package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/storage"
)

// debounceDelay is the quiet period a path must observe before the watcher
// reindexes it. It coalesces the burst of events an editor's atomic save emits
// (temp write + rename → Create/Rename/Write) into a single reindex.
const debounceDelay = 500 * time.Millisecond

// WatchConfig carries the selection and chunking settings the watcher reuses
// from the batch pipeline, plus the storage directory to ignore so the watcher
// never reacts to its own database writes.
type WatchConfig struct {
	// Include/Exclude/SecurityExclude are the same glob sets the scanner uses;
	// the watcher applies them through the shared Match predicate so a live event
	// and a batch scan agree on what is indexable.
	Include         []string
	Exclude         []string
	SecurityExclude []string
	// Chunking is the token budget for reindexing a changed file.
	Chunking chunk.Config
	// StorageDir is the directory holding the SQLite database (e.g. ".mnemos").
	// Events under it are ignored so the watcher does not churn on WAL/SHM writes
	// it triggers itself.
	StorageDir string
	// MaxFileBytes caps the size of a single file read into memory; a larger file
	// is skipped with a warning. A value <= 0 disables the cap, matching the
	// [indexing].max_file_bytes config contract.
	MaxFileBytes int64
}

// Watcher incrementally keeps a collection in sync with a directory tree. It
// performs a startup reconcile (full hash-skip scan plus removal of vanished
// documents) and then watches for live changes, reindexing modified files and
// deleting removed ones. It reuses the Phase 1 pipeline for all ingestion, so it
// adds no parsing or chunking logic of its own.
type Watcher struct {
	db         *sql.DB
	logger     *slog.Logger
	root       string
	collection string
	cfg        WatchConfig
	pipeline   *Pipeline
	debouncer  *debouncer
	// ready is closed once Run has finished the startup reconcile and registered
	// the live filesystem watch. After it is closed, changes to the tree are
	// reliably observed. See Ready.
	ready chan struct{}
}

// NewWatcher builds a Watcher over db. root is the directory to watch (relative
// or absolute; it is resolved to an absolute path), collection the logical space
// reindexed files belong to, and cfg the shared selection/chunking settings.
func NewWatcher(db *sql.DB, logger *slog.Logger, root, collection string, cfg WatchConfig) (*Watcher, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("watch: abs %q: %w", root, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("watch: stat %q: %w", abs, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("watch: %q is not a directory", abs)
	}

	// MaxFileBytes carries the same contract as the config and ingest paths: a
	// value <= 0 disables the cap, > 0 sets it. Pass it straight through so
	// `watch` honors `max_file_bytes = 0` (disable) identically to `ingest`.
	return &Watcher{
		db:         db,
		logger:     logger,
		root:       abs,
		collection: collection,
		cfg:        cfg,
		pipeline:   New(db, logger, WithMaxFileBytes(cfg.MaxFileBytes)),
		debouncer:  newDebouncer(debounceDelay),
		ready:      make(chan struct{}),
	}, nil
}

// Ready returns a channel that is closed once Run has completed its startup
// reconcile and registered the live filesystem watch. Until then, file changes
// may not yet be observed; after it is closed, they are. Callers that mutate the
// watched tree and expect the watcher to react (notably tests, but also any
// orchestrator that wants to know the watcher is live) can block on it. If Run
// returns before going live (a reconcile or watch-setup error), the channel is
// never closed; select on it together with the Run error or a timeout.
func (w *Watcher) Ready() <-chan struct{} { return w.ready }

// Run reconciles the store against disk, then watches for live changes until ctx
// is cancelled. On cancellation it shuts down cleanly: the fsnotify watcher is
// closed and any in-flight debounce timers are drained.
func (w *Watcher) Run(ctx context.Context) error {
	if err := w.reconcile(ctx); err != nil {
		return err
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("watch: new fsnotify watcher: %w", err)
	}
	defer func() { _ = fsw.Close() }()
	defer w.debouncer.Stop()

	if err := w.addTree(fsw); err != nil {
		return err
	}

	// The reconcile is done and every directory is registered: the watch is now
	// live, so changes from here on are reliably delivered. Signal readiness.
	close(w.ready)

	w.logger.Info("watch live", "root", w.root, "collection", w.collection)

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("watch shutting down", "root", w.root)

			return nil
		case event, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			w.handleEvent(ctx, fsw, event)
		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			w.logger.Warn("watch fsnotify error", "error", err)
		}
	}
}

// reconcile runs a full hash-skip scan of root, then removes any document in the
// collection whose backing file no longer exists. Hash-skip makes the scan cheap
// on restart: unchanged files are no-ops, so this safely re-syncs the store.
func (w *Watcher) reconcile(ctx context.Context) error {
	summary, err := w.pipeline.Run(ctx, Options{
		Root:       w.root,
		Collection: w.collection,
		Rules: Rules{
			Include:         w.cfg.Include,
			Exclude:         w.cfg.Exclude,
			SecurityExclude: w.cfg.SecurityExclude,
		},
		Chunking: w.cfg.Chunking,
	})
	if err != nil {
		return fmt.Errorf("watch: reconcile scan: %w", err)
	}
	w.logger.Info("watch reconcile scan",
		"scanned", summary.FilesScanned,
		"ingested", summary.FilesIngested,
		"skipped", summary.FilesSkipped,
	)

	return w.removeVanished(ctx)
}

// removeVanished deletes documents in the collection whose uri resolves to a
// file that no longer exists under root. The uri is root-relative (matching how
// ingest stores it), so resolving it against root yields the absolute path to
// stat.
func (w *Watcher) removeVanished(ctx context.Context) error {
	uris, err := storage.ListURIsByCollection(ctx, w.db, w.collection)
	if err != nil {
		return fmt.Errorf("watch: list documents: %w", err)
	}
	var vanished []string
	for _, uri := range uris {
		abs := filepath.Join(w.root, filepath.FromSlash(uri))
		if _, err = os.Stat(abs); errors.Is(err, os.ErrNotExist) {
			vanished = append(vanished, uri)
		}
	}
	if len(vanished) == 0 {
		return nil
	}

	// Delete the whole batch in one transaction so a mid-run termination leaves
	// the index either fully reconciled or untouched, never half-pruned.
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("watch: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit
	for _, uri := range vanished {
		if err := storage.DeleteByURITx(ctx, tx, uri); err != nil {
			return fmt.Errorf("watch: remove vanished %q: %w", uri, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("watch: commit vanished removals: %w", err)
	}
	for _, uri := range vanished {
		w.logger.Info("watch removed vanished document", "uri", uri)
	}

	return nil
}

// addTree walks root and registers every directory with the fsnotify watcher.
// fsnotify is non-recursive, so each directory is added individually; new
// directories are added on the fly in handleEvent. The storage directory is
// skipped so the watcher never sees its own database writes.
func (w *Watcher) addTree(fsw *fsnotify.Watcher) error {
	walkErr := filepath.WalkDir(w.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if w.isStorageDir(path) {
			return filepath.SkipDir
		}
		if err := fsw.Add(path); err != nil {
			return fmt.Errorf("watch: add %q: %w", path, err)
		}

		return nil
	})
	if walkErr != nil {
		return fmt.Errorf("watch: walk %q: %w", w.root, walkErr)
	}

	return nil
}

// handleEvent routes one fsnotify event. New directories are registered for
// watching; create/write/rename of an indexable file schedule a debounced
// reindex; remove/rename-away of a tracked file schedule a debounced deletion.
// Events under the storage directory or on the database files are ignored.
func (w *Watcher) handleEvent(ctx context.Context, fsw *fsnotify.Watcher, event fsnotify.Event) {
	path := event.Name
	if w.isIgnored(path) {
		return
	}

	// A newly created directory must be added to the watch set (recursion is
	// manual). Its already-present children are picked up by their own events.
	if event.Op.Has(fsnotify.Create) {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			if err := fsw.Add(path); err != nil {
				w.logger.Warn("watch add new dir", "path", path, "error", err)
			}

			return
		}
	}

	rel, err := filepath.Rel(w.root, path)
	if err != nil {
		w.logger.Warn("watch relativize", "path", path, "error", err)

		return
	}
	uri := filepath.ToSlash(rel)

	// Remove / rename-away: the file is gone (or moved out from under this name).
	// Stat decides between "gone" and "still here after a rename-to"; debounce
	// the eviction so a rapid delete+recreate settles to the final state.
	if event.Op.Has(fsnotify.Remove) || event.Op.Has(fsnotify.Rename) {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			w.debouncer.trigger(path, func() { w.deletePath(ctx, uri) })

			return
		}
	}

	// Write / create / rename-to of a file that passes the include-exclude
	// predicate: reindex it. Hash-skip makes an unchanged file a no-op.
	if !Match(rel, w.cfg.Include, w.cfg.Exclude, w.cfg.SecurityExclude) {
		return
	}
	w.debouncer.trigger(path, func() { w.reindexPath(ctx, path, uri) })
}

// reindexPath ingests a single file through the shared one-shot path. An
// unchanged file is a no-op (hash-skip). A file that vanished between the event
// and the debounced callback is ignored.
func (w *Watcher) reindexPath(ctx context.Context, absPath, uri string) {
	if _, err := os.Stat(absPath); errors.Is(err, os.ErrNotExist) {
		return
	}
	docID, chunks, err := w.pipeline.IngestPath(ctx, absPath, uri, w.collection, w.cfg.Chunking)
	if err != nil {
		w.logger.Warn("watch reindex failed", "uri", uri, "error", err)

		return
	}
	w.logger.Debug("watch reindexed", "uri", uri, "document_id", docID, "chunks", chunks)
}

// deletePath evicts a document by uri. The DELETE cascades to chunks/links and
// the FTS index via foreign keys and the chunks delete trigger.
func (w *Watcher) deletePath(ctx context.Context, uri string) {
	if err := storage.DeleteByURI(ctx, w.db, uri); err != nil {
		w.logger.Warn("watch delete failed", "uri", uri, "error", err)

		return
	}
	w.logger.Info("watch removed document", "uri", uri)
}

// isIgnored reports whether path is the storage directory itself, a path under
// it, or a SQLite database file (mnemos.db, -wal, -shm). Ignoring these prevents
// the watcher from reacting to its own database writes (an event storm).
func (w *Watcher) isIgnored(path string) bool {
	if w.isStorageDir(path) {
		return true
	}
	if w.cfg.StorageDir != "" {
		storageAbs := w.absStorageDir()
		if storageAbs != "" && strings.HasPrefix(path, storageAbs+string(os.PathSeparator)) {
			return true
		}
	}
	base := filepath.Base(path)

	return strings.HasPrefix(base, "mnemos.db")
}

// isStorageDir reports whether path is the configured storage directory.
func (w *Watcher) isStorageDir(path string) bool {
	storageAbs := w.absStorageDir()

	return storageAbs != "" && path == storageAbs
}

// absStorageDir resolves the configured storage directory to an absolute path,
// or "" when no storage directory is configured.
func (w *Watcher) absStorageDir() string {
	if w.cfg.StorageDir == "" {
		return ""
	}
	if filepath.IsAbs(w.cfg.StorageDir) {
		return filepath.Clean(w.cfg.StorageDir)
	}

	return filepath.Join(w.root, w.cfg.StorageDir)
}
