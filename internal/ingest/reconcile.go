package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/arhuman/mnemos/internal/storage"
)

// ReconcileSummary reports what a reconcile pass removed.
type ReconcileSummary struct {
	// Considered is how many indexed documents were in scope.
	Considered int
	// Removed is how many were evicted because their backing file is gone.
	Removed int
	// URIs are the evicted URIs, in the order they were removed.
	URIs []string
}

// ErrReconcileRootMissing means the scan root itself is unreadable, so absence of
// a file below it proves nothing. Callers must not treat it as "everything was
// deleted": an unmounted volume and an emptied directory are indistinguishable
// from inside the walk, and evicting a namespace is not recoverable by a reindex.
var ErrReconcileRootMissing = errors.New("ingest: reconcile root is missing or unreadable")

// Reconcile evicts every indexed document under uriPrefix whose backing file no
// longer exists on disk. It is the deletion half of indexing: Run and
// ReindexContent are additive, so without this pass a deleted file keeps being
// returned by search and cited with line ranges into a file that is gone (#42).
//
// uriBase is the directory URIs are relative to (the kb root). uriPrefix scopes
// the pass to a subtree, slash-separated and matched at segment boundaries, so
// "adr" covers "adr/0001.md" but never "adr-archive/x.md"; empty means the whole
// store. root is the directory whose readability gates the pass.
//
// It returns ErrReconcileRootMissing without evicting anything when root cannot
// be read. Every eviction commits in one transaction, so an interrupted run
// leaves the index either fully reconciled or untouched, never half-pruned.
func Reconcile(ctx context.Context, db *sql.DB, root, uriBase, uriPrefix string) (ReconcileSummary, error) {
	// Gate on the root before trusting any per-file absence below it.
	if _, err := os.Stat(root); err != nil {
		return ReconcileSummary{}, fmt.Errorf("%w: %q: %w", ErrReconcileRootMissing, root, err)
	}

	uris, err := storage.ListURIs(ctx, db)
	if err != nil {
		return ReconcileSummary{}, err
	}

	var sum ReconcileSummary
	var vanished []string
	for _, uri := range uris {
		if !uriUnderPrefix(uri, uriPrefix) {
			continue
		}
		sum.Considered++
		abs := filepath.Join(uriBase, filepath.FromSlash(uri))
		// Stat, not Lstat: it follows symlinks, so a link whose target was deleted
		// counts as vanished even though the link file itself still exists.
		if _, err := os.Stat(abs); errors.Is(err, os.ErrNotExist) {
			vanished = append(vanished, uri)
		}
	}
	if len(vanished) == 0 {
		return sum, nil
	}

	if err := deleteURIBatch(ctx, db, vanished); err != nil {
		return sum, err
	}
	sum.Removed = len(vanished)
	sum.URIs = vanished

	return sum, nil
}

// deleteURIBatch removes every uri in one transaction.
func deleteURIBatch(ctx context.Context, db *sql.DB, uris []string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ingest: reconcile begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit
	for _, uri := range uris {
		if err := storage.DeleteByURITx(ctx, tx, uri); err != nil {
			return fmt.Errorf("ingest: reconcile remove %q: %w", uri, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ingest: reconcile commit: %w", err)
	}

	return nil
}

// uriUnderPrefix reports whether uri is prefix itself or sits beneath it at a
// segment boundary. An empty prefix matches everything. A trailing slash is
// ignored, so "adr" and "adr/" behave identically.
func uriUnderPrefix(uri, prefix string) bool {
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" || prefix == "." {
		return true
	}

	return uri == prefix || strings.HasPrefix(uri, prefix+"/")
}

// uriPrefixFor derives the reconcile scope from a scan root: the slash-relative
// path of root under uriBase, or "" when root is uriBase itself. A root outside
// uriBase yields ok=false, so a caller reconciles nothing rather than guessing a
// scope (out-of-tree roots mint URIs this pass cannot resolve anyway).
func uriPrefixFor(uriBase, root string) (prefix string, ok bool) {
	rel, err := filepath.Rel(uriBase, root)
	if err != nil {
		return "", false
	}
	if rel == "." {
		return "", true
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}

	return filepath.ToSlash(rel), true
}
