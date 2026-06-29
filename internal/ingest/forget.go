package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/arhuman/mnemos/internal/storage"
)

// ForgetPath deletes a file from the OKF tree: it removes the document from the
// index and the backing file from disk, returning whether a file was actually
// removed. It is idempotent — forgetting a path whose file is already gone is not
// an error and reports deleted=false. Both the CLI `forget` command and the
// mnemos.forget MCP tool route through it, so the two surfaces cannot drift in
// behavior or crash semantics.
//
// The index is cleared BEFORE the file is removed. A DB failure before any disk
// mutation leaves the system coherent: the file and its index entry both still
// exist, so a re-run completes cleanly. The reverse order risks a ghost document
// — indexed but missing on disk — that survives until the next reconcile. The
// opposite residue, an orphaned file present on disk but not indexed, is benign:
// the next ingest or watcher reconcile re-indexes or re-removes it automatically.
func ForgetPath(ctx context.Context, db *sql.DB, logger *slog.Logger, abs, uri string) (bool, error) {
	if err := storage.DeleteByURI(ctx, db, uri); err != nil {
		return false, fmt.Errorf("forget: deindex %q: %w", uri, err)
	}

	deleted := true
	if err := os.Remove(abs); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("forget: remove %q: %w", uri, err)
		}
		deleted = false
	}

	logger.Info("forget removed file", "uri", uri, "deleted", deleted)

	return deleted, nil
}
