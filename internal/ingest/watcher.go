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
	"github.com/arhuman/mnemos/internal/security"
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
	// Encoding declares legacy source charsets, so a live edit to a non-UTF-8
	// file is decoded exactly as a batch ingest would decode it.
	Encoding []EncodingRule
	// StorageDir is the directory holding the SQLite database (e.g. ".mnemos").
	// Events under it are ignored so the watcher does not churn on WAL/SHM writes
	// it triggers itself.
	StorageDir string
	// URIBase is the directory document URIs are made relative to (the kb root).
	// Empty means use the watched root, reproducing root-relative URIs.
	URIBase string
	// MaxFileBytes caps the size of a single file read into memory; a larger file
	// is skipped with a warning. A value <= 0 disables the cap, matching the
	// [indexing].max_file_bytes config contract.
	MaxFileBytes int64
	// ScanSecrets screens a changed file for credentials before indexing it, so a
	// live edit is held to the same bar as a batch ingest. Mirrors
	// [security].exclude_secrets.
	ScanSecrets bool
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
	uriBase    string
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

	uriBase := abs
	if cfg.URIBase != "" {
		if uriBase, err = filepath.Abs(cfg.URIBase); err != nil {
			return nil, fmt.Errorf("watch: abs uri base %q: %w", cfg.URIBase, err)
		}
	}

	// MaxFileBytes carries the same contract as the config and ingest paths: a
	// value <= 0 disables the cap, > 0 sets it. Pass it straight through so
	// `watch` honors `max_file_bytes = 0` (disable) identically to `ingest`.
	popts := []Option{WithMaxFileBytes(cfg.MaxFileBytes), WithEncodings(cfg.Encoding)}
	if cfg.ScanSecrets {
		popts = append(popts, WithSecretScanner(security.NewRegexScanner()))
	}
	pipeline := New(db, logger, popts...)
	// Report a bad charset here rather than at the first matching file: the
	// watcher is long-running, so a deferred error would surface as an unexplained
	// per-file skip long after startup.
	if pipeline.encodingErr != nil {
		return nil, fmt.Errorf("watch: %w", pipeline.encodingErr)
	}

	return &Watcher{
		db:         db,
		logger:     logger,
		root:       abs,
		uriBase:    uriBase,
		collection: collection,
		cfg:        cfg,
		pipeline:   pipeline,
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
		URIBase:    w.uriBase,
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
		abs := filepath.Join(w.uriBase, filepath.FromSlash(uri))
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

// addTreeAndSweep registers dir and every directory beneath it with the fsnotify
// watcher, then schedules a debounced reindex for every indexable file already
// inside. The sweep is what makes a directory moved into the tree converge: its
// contents arrive atomically with the rename and generate no events of their own,
// so only an explicit walk can discover them. A freshly created empty directory
// sweeps nothing, making this a superset of a bare fsw.Add.
//
// Failures are logged rather than returned: a live event handler cannot abort the
// watch loop, and the startup reconcile is the backstop for anything missed here.
func (w *Watcher) addTreeAndSweep(ctx context.Context, fsw *fsnotify.Watcher, dir string) {
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if w.isIgnored(path) {
			if d.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}
		if d.IsDir() {
			if aerr := fsw.Add(path); aerr != nil {
				w.logger.Warn("watch add new dir", "path", path, "error", aerr)
			}

			return nil
		}

		rel, rerr := filepath.Rel(w.root, path)
		if rerr != nil {
			w.logger.Warn("watch relativize", "path", path, "error", rerr)

			return nil
		}
		if !Match(rel, w.cfg.Include, w.cfg.Exclude, w.cfg.SecurityExclude) {
			return nil
		}
		uriRel, uerr := filepath.Rel(w.uriBase, path)
		if uerr != nil {
			w.logger.Warn("watch relativize uri", "path", path, "error", uerr)

			return nil
		}
		uri := filepath.ToSlash(uriRel)
		w.debouncer.trigger(path, func() { w.reindexPath(ctx, path, uri) })

		return nil
	})
	if err != nil {
		w.logger.Warn("watch sweep new dir", "path", dir, "error", err)
	}
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
	// manual), and its existing children swept. A mkdir arrives empty and its
	// children generate their own Create events, but a directory renamed or moved
	// into the tree arrives already populated and fsnotify emits nothing for the
	// contents — so without the sweep those files stay invisible until restart.
	if event.Op.Has(fsnotify.Create) {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			w.addTreeAndSweep(ctx, fsw, path)

			return
		}
	}

	rel, err := filepath.Rel(w.root, path)
	if err != nil {
		w.logger.Warn("watch relativize", "path", path, "error", err)

		return
	}
	// Match (glob anchoring) uses the root-relative path; the stored URI uses the
	// uriBase-relative path so a subtree watch still mints kb-relative URIs.
	uriRel, err := filepath.Rel(w.uriBase, path)
	if err != nil {
		w.logger.Warn("watch relativize uri", "path", path, "error", err)

		return
	}
	uri := filepath.ToSlash(uriRel)

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

// deletePath evicts whatever was indexed at uri. The DELETE cascades to
// chunks/links and the FTS index via foreign keys and the chunks delete trigger.
//
// A vanished path may have been either a file or a directory, and fsnotify does
// not say which (it cannot stat what is already gone). DeleteByURI matches the
// uri exactly, so a directory uri would match no document and evict nothing,
// leaving every document beneath it indexed at a path that no longer exists.
// The prefix sweep is therefore not an optimization: it is what makes a removed
// or renamed-away directory converge. It mirrors moveDir's prefix handling, so
// the watcher and `mnemos mv` agree on what a directory-shaped change means.
func (w *Watcher) deletePath(ctx context.Context, uri string) {
	if err := storage.DeleteByURI(ctx, w.db, uri); err != nil {
		w.logger.Warn("watch delete failed", "uri", uri, "error", err)

		return
	}
	w.logger.Info("watch removed document", "uri", uri)

	w.deleteSubtree(ctx, uri)
}

// deleteSubtree evicts every document indexed beneath uri, treating it as a
// directory prefix. It is a no-op for a plain file uri (nothing is stored under
// "notes/a.md/"), so it is safe to call unconditionally after a document delete.
func (w *Watcher) deleteSubtree(ctx context.Context, uri string) {
	prefix := uri + "/"
	rows, err := storage.ListDocuments(ctx, w.db, storage.ListFilter{PathPrefix: prefix})
	if err != nil {
		w.logger.Warn("watch list subtree failed", "prefix", prefix, "error", err)

		return
	}
	for _, row := range rows {
		if err := storage.DeleteByURI(ctx, w.db, row.URI); err != nil {
			w.logger.Warn("watch delete failed", "uri", row.URI, "error", err)

			continue
		}
		w.logger.Info("watch removed document", "uri", row.URI)
	}
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
