package ingest

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/storage"
)

// MoveEntry records one file relocated by MovePath: its old and new uris and the
// document id it was re-indexed under (the id derives from collection + uri, so
// it changes on every move).
type MoveEntry struct {
	From       string
	To         string
	DocumentID string
}

// MoveResult is the outcome of a MovePath call. IsDir reports whether the source
// was a directory (a subtree move). DanglingLinks is the total number of inbound
// markdown links left pointing at the old uris — not rewritten in V0.
type MoveResult struct {
	Entries       []MoveEntry
	DanglingLinks int
	IsDir         bool
}

// MovePath moves a file or directory within the OKF tree: it renames the path on
// disk and re-indexes every affected indexed document under its new uri,
// preserving each document's collection. For a directory it relocates the whole
// subtree and re-indexes each document that was indexed under the old prefix
// (files present on disk but never indexed simply move with the subtree). Both
// the CLI `mv` command and the mnemos.move MCP tool route through it, so the two
// surfaces cannot drift.
//
// Inbound markdown links to moved uris are not rewritten (a V0 limitation); their
// total is returned in MoveResult.DanglingLinks and logged as a warning.
func MovePath(ctx context.Context, db *sql.DB, logger *slog.Logger, absFrom, absTo, oldURI, newURI string, cfg chunk.Config) (MoveResult, error) {
	info, err := os.Stat(absFrom)
	if err != nil {
		return MoveResult{}, fmt.Errorf("move: stat %q: %w", oldURI, err)
	}
	if err := os.MkdirAll(filepath.Dir(absTo), 0o750); err != nil {
		return MoveResult{}, fmt.Errorf("move: mkdir %q: %w", newURI, err)
	}

	if !info.IsDir() {
		entry, dangling, ferr := moveOneFile(ctx, db, logger, absFrom, absTo, oldURI, newURI, cfg)
		if ferr != nil {
			return MoveResult{}, ferr
		}

		return MoveResult{Entries: []MoveEntry{entry}, DanglingLinks: dangling}, nil
	}

	return moveDir(ctx, db, logger, absTo, oldURI, newURI, cfg, absFrom)
}

// moveOneFile relocates a single file: it preserves the source's collection,
// renames it on disk, then de-indexes the old uri and re-indexes it under the
// new uri. It returns the count of inbound links left dangling for the caller to
// aggregate. The rename happens first so the most likely failure (cross-device,
// permissions, destination exists) leaves the index completely untouched — a
// failed move never silently drops the document from search. Only after the file
// is safely at its new path is the index updated; if the re-index step then
// fails, the worst case is a file on disk that is not yet re-indexed, a benign,
// re-runnable state.
func moveOneFile(ctx context.Context, db *sql.DB, logger *slog.Logger, absFrom, absTo, oldURI, newURI string, cfg chunk.Config) (MoveEntry, int, error) {
	collection, ok, err := storage.CollectionByURI(ctx, db, oldURI)
	if err != nil {
		return MoveEntry{}, 0, fmt.Errorf("move: lookup collection: %w", err)
	}
	if !ok {
		collection = "default"
	}

	dangling := 0
	if inbound, lerr := storage.CountInboundLinks(ctx, db, oldURI); lerr != nil {
		logger.Warn("move: count inbound links failed", "uri", oldURI, "err", lerr)
	} else {
		dangling = inbound
	}

	if err = os.Rename(absFrom, absTo); err != nil {
		return MoveEntry{}, 0, fmt.Errorf("move: rename %q -> %q: %w", oldURI, newURI, err)
	}
	if err = storage.DeleteByURI(ctx, db, oldURI); err != nil {
		return MoveEntry{}, 0, fmt.Errorf("move: deindex %q: %w", oldURI, err)
	}
	docID, _, err := File(ctx, db, logger, absTo, newURI, collection, cfg)
	if err != nil {
		return MoveEntry{}, 0, fmt.Errorf("move: reindex %q: %w", newURI, err)
	}

	if dangling > 0 {
		logger.Warn("move leaves inbound links dangling (not rewritten in V0)",
			"from", oldURI, "to", newURI, "inbound_links", dangling)
	}
	logger.Info("move relocated file", "from", oldURI, "to", newURI, "collection", collection)

	return MoveEntry{From: oldURI, To: newURI, DocumentID: docID}, dangling, nil
}

// moveDir relocates a directory subtree. It snapshots the indexed documents under
// the old prefix before the rename, moves the subtree in one os.Rename, then
// de-indexes each old uri and re-indexes the file at its new path under the same
// collection.
func moveDir(ctx context.Context, db *sql.DB, logger *slog.Logger, absTo, oldURI, newURI string, cfg chunk.Config, absFrom string) (MoveResult, error) {
	prefix := oldURI + "/"
	rows, err := storage.ListDocuments(ctx, db, storage.ListFilter{PathPrefix: prefix})
	if err != nil {
		return MoveResult{}, fmt.Errorf("move: list %q: %w", prefix, err)
	}

	res := MoveResult{IsDir: true}

	// Phase 0: count inbound links across the whole set BEFORE any mutation.
	// Counting up front (rather than interleaved with the deletes) is what makes
	// the tally correct: a link from one moved document to another is still
	// counted, whereas deleting a document first would drop its outbound edges
	// from the count and under-report the breakage.
	for _, row := range rows {
		if inbound, lerr := storage.CountInboundLinks(ctx, db, row.URI); lerr != nil {
			logger.Warn("move: count inbound links failed", "uri", row.URI, "err", lerr)
		} else {
			res.DanglingLinks += inbound
		}
	}

	// Rename the subtree first so the most likely failure (cross-device,
	// permissions, destination exists) leaves the index completely untouched and
	// the move is a no-op rather than a silent drop of every document.
	if err := os.Rename(absFrom, absTo); err != nil {
		return MoveResult{}, fmt.Errorf("move: rename %q -> %q: %w", oldURI, newURI, err)
	}

	// Phase 1: de-index every old uri now that the files have moved. A failure
	// here can only leave files on disk not yet re-indexed — a benign,
	// re-runnable state (a watcher would pick them up).
	for _, row := range rows {
		if err := storage.DeleteByURI(ctx, db, row.URI); err != nil {
			return MoveResult{}, fmt.Errorf("move: deindex %q: %w", row.URI, err)
		}
	}

	// Phase 2: re-index each moved file under its new uri, preserving collection.
	// This is best-effort: a file that fails to re-index is logged and recorded,
	// but the loop continues so one bad file cannot strand the rest un-indexed.
	// Failed files remain on disk (already moved) and un-indexed — a benign,
	// re-runnable state. Any failures are reported as an aggregated error after
	// the successful entries are populated, so the caller still sees what moved.
	newPrefix := newURI + "/"
	var failed []string
	for _, row := range rows {
		suffix := strings.TrimPrefix(row.URI, prefix)
		nu := newPrefix + suffix
		abs := filepath.Join(absTo, filepath.FromSlash(suffix))

		docID, _, ierr := File(ctx, db, logger, abs, nu, row.Collection, cfg)
		if ierr != nil {
			logger.Error("move: re-index failed; file moved but left un-indexed",
				"uri", nu, "err", ierr)
			failed = append(failed, nu)

			continue
		}
		res.Entries = append(res.Entries, MoveEntry{From: row.URI, To: nu, DocumentID: docID})
	}

	if res.DanglingLinks > 0 {
		logger.Warn("move leaves inbound links dangling (not rewritten in V0)",
			"from", oldURI, "to", newURI, "inbound_links", res.DanglingLinks)
	}
	logger.Info("move relocated directory", "from", oldURI, "to", newURI,
		"reindexed", len(res.Entries), "failed", len(failed))

	if len(failed) > 0 {
		return res, fmt.Errorf("move: %d of %d files moved but could not be re-indexed (left un-indexed on disk): %s",
			len(failed), len(rows), strings.Join(failed, ", "))
	}

	return res, nil
}
