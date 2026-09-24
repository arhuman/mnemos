package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/arhuman/mnemos/internal/model"
)

// JournalDocType is the OKF `type` a document declares to be a journal entry.
// Matching is case-insensitive, like every other type comparison in the index,
// so `Journal`, `journal` and `JOURNAL` are one type.
const JournalDocType = "journal"

// IsJournalType reports whether an OKF document type names the journal tier.
// It is the single place the spelling is decided, so ingest, promotion and any
// future surface cannot disagree about what counts as a journal entry.
func IsJournalType(docType string) bool {
	return strings.EqualFold(strings.TrimSpace(docType), JournalDocType)
}

// ListJournal returns journal documents ordered newest first, capped at limit
// (a limit <= 0 returns all of them). It reads the stored journal flag rather
// than matching a uri prefix, so an entry stays listed after a move or a rename
// (ADR-0011).
//
// Entries are returned whether or not they have been promoted; the frontmatter
// key PromotedToKey on each says which. Consolidation filters on it so a second
// pass does not re-promote the same material (ADR-0011).
func ListJournal(ctx context.Context, db *sql.DB, limit int) ([]model.Document, error) {
	q := `SELECT id, uri, collection, content_hash, COALESCE(title, ''),
	             COALESCE(mime_type, ''), size_bytes, COALESCE(modified_at, ''),
	             indexed_at, COALESCE(frontmatter_json, '')
	      FROM documents WHERE journal = 1
	      ORDER BY COALESCE(modified_at, indexed_at) DESC, uri`
	args := []any{}
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list journal: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []model.Document
	for rows.Next() {
		var d model.Document
		if err := rows.Scan(
			&d.ID, &d.URI, &d.Collection, &d.ContentHash, &d.Title,
			&d.MimeType, &d.SizeBytes, &d.ModifiedAt, &d.IndexedAt, &d.FrontmatterJSON,
		); err != nil {
			return nil, fmt.Errorf("storage: scan journal row: %w", err)
		}
		d.Journal = true
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate journal rows: %w", err)
	}

	return out, nil
}

// CountJournal returns how many journal documents are indexed. It exists for
// the doctor/stats surfaces, which want the size of the tier without paging
// every row through memory.
func CountJournal(ctx context.Context, db *sql.DB) (int, error) {
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM documents WHERE journal = 1`).Scan(&n); err != nil {
		return 0, fmt.Errorf("storage: count journal: %w", err)
	}

	return n, nil
}

// PromotedToKey is the frontmatter key a journal entry carries once its content
// has been promoted into the knowledge base, naming the kb document that now
// holds it. Its presence is what stops a second consolidation pass from
// re-promoting the same material (ADR-0011).
const PromotedToKey = "promoted_to"

// CapturedAtKey is the frontmatter key holding when the entry was captured. It
// is distinct from the document's modified_at, which a later edit or a re-ingest
// moves: the capture instant must not drift.
const CapturedAtKey = "captured_at"

// IsJournalURI reports whether the document at uri is a journal entry. A uri
// with no document returns false: absent is not journal.
func IsJournalURI(ctx context.Context, db *sql.DB, uri string) (bool, error) {
	var j bool
	err := db.QueryRowContext(ctx,
		`SELECT journal FROM documents WHERE uri = ?`, uri).Scan(&j)
	switch {
	case err == sql.ErrNoRows:
		return false, nil
	case err != nil:
		return false, fmt.Errorf("storage: journal flag for %q: %w", uri, err)
	default:
		return j, nil
	}
}
