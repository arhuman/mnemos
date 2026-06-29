package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/arhuman/mnemos/internal/model"
)

// DocumentHashByURI returns the stored content_hash for a uri, or ("", false)
// when no document with that uri exists. The ingest pipeline uses it to skip
// unchanged files before parsing.
func DocumentHashByURI(ctx context.Context, db *sql.DB, uri string) (string, bool, error) {
	var hash string
	err := db.QueryRowContext(ctx, `SELECT content_hash FROM documents WHERE uri = ?`, uri).Scan(&hash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("storage: lookup hash for %q: %w", uri, err)
	default:
		return hash, true, nil
	}
}

// CollectionByURI returns the stored collection for a uri, or ("", false) when
// no document with that uri exists. move uses it to preserve a document's
// collection when re-ingesting it under a new uri (the document id is derived
// from collection + uri, so a move is a delete-old + ingest-new).
func CollectionByURI(ctx context.Context, db *sql.DB, uri string) (string, bool, error) {
	var collection string
	err := db.QueryRowContext(ctx, `SELECT collection FROM documents WHERE uri = ?`, uri).Scan(&collection)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("storage: lookup collection for %q: %w", uri, err)
	default:
		return collection, true, nil
	}
}

// CountInboundLinks returns how many link edges point at dstURI (links.dst_doc
// is a plain uri string with no foreign key). move reports this as the number of
// inbound markdown links left dangling after a rename (a V0 limitation).
func CountInboundLinks(ctx context.Context, db *sql.DB, dstURI string) (int, error) {
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM links WHERE dst_doc = ?`, dstURI).Scan(&n); err != nil {
		return 0, fmt.Errorf("storage: count inbound links for %q: %w", dstURI, err)
	}

	return n, nil
}

// UpsertDocument inserts or updates a document by uri within tx. On conflict it
// refreshes the mutable fields (hash, title, sizes, timestamps, frontmatter)
// and re-stamps indexed_at, keeping the existing id stable.
func UpsertDocument(ctx context.Context, tx *sql.Tx, d model.Document) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO documents (
			id, uri, collection, content_hash, title, mime_type,
			size_bytes, modified_at, indexed_at, frontmatter_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(uri) DO UPDATE SET
			content_hash     = excluded.content_hash,
			collection       = excluded.collection,
			title            = excluded.title,
			mime_type        = excluded.mime_type,
			size_bytes       = excluded.size_bytes,
			modified_at      = excluded.modified_at,
			indexed_at       = excluded.indexed_at,
			frontmatter_json = excluded.frontmatter_json
	`,
		d.ID, d.URI, d.Collection, d.ContentHash, nullString(d.Title), nullString(d.MimeType),
		d.SizeBytes, nullString(d.ModifiedAt), d.IndexedAt, nullString(d.FrontmatterJSON),
	)
	if err != nil {
		return fmt.Errorf("storage: upsert document %q: %w", d.URI, err)
	}

	return nil
}

// DeleteByURI removes the document with the given uri. Its chunks and links are
// removed by the ON DELETE CASCADE foreign keys, and the chunks delete trigger
// (chunks_ad) cleans the FTS external-content index — so a single DELETE on
// documents fully evicts the document from every searchable surface. Deleting a
// uri that does not exist is a no-op (no error). The watcher calls this when a
// tracked file vanishes from disk.
func DeleteByURI(ctx context.Context, db *sql.DB, uri string) error {
	return execDeleteByURI(ctx, db, uri)
}

// DeleteByURITx is DeleteByURI within a caller-managed transaction, so a batch
// of deletes (e.g. the watcher's vanished-file reconcile) commits atomically.
func DeleteByURITx(ctx context.Context, tx *sql.Tx, uri string) error {
	return execDeleteByURI(ctx, tx, uri)
}

// execer is the ExecContext subset shared by *sql.DB and *sql.Tx.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func execDeleteByURI(ctx context.Context, e execer, uri string) error {
	if _, err := e.ExecContext(ctx, `DELETE FROM documents WHERE uri = ?`, uri); err != nil {
		return fmt.Errorf("storage: delete document %q: %w", uri, err)
	}

	return nil
}

// ListURIsByCollection returns every documents.uri in the given collection,
// unordered. The watcher uses it during startup reconcile to find documents
// whose backing file has disappeared from disk.
func ListURIsByCollection(ctx context.Context, db *sql.DB, collection string) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT uri FROM documents WHERE collection = ?`, collection)
	if err != nil {
		return nil, fmt.Errorf("storage: list uris for collection %q: %w", collection, err)
	}
	defer func() { _ = rows.Close() }()

	var uris []string
	for rows.Next() {
		var uri string
		if err := rows.Scan(&uri); err != nil {
			return nil, fmt.Errorf("storage: scan uri for collection %q: %w", collection, err)
		}
		uris = append(uris, uri)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate uris for collection %q: %w", collection, err)
	}

	return uris, nil
}

// DocumentRow is a document's stored metadata as returned by ListDocuments. The
// type/tags a caller may want live inside FrontmatterJSON (the raw YAML
// re-encoded as JSON); decode it on the caller side rather than joining chunks.
type DocumentRow struct {
	URI             string
	Collection      string
	Title           string
	ModifiedAt      string
	IndexedAt       string
	FrontmatterJSON string
	SizeBytes       int64
}

// ListFilter narrows ListDocuments. Empty string fields are ignored; Limit <= 0
// returns all matching rows.
type ListFilter struct {
	Collection string // exact collection match
	PathPrefix string // documents.uri starts with this slash-relative prefix
	FileType   string // file extension without the dot (e.g. "md")
	Limit      int
}

// ListDocuments returns document metadata rows ordered by uri, narrowed by the
// filter. It backs the browse/list feature and directory move (which lists every
// document under a uri prefix to re-index it). title/modified_at may be NULL in
// the schema, so they are scanned through sql.NullString.
func ListDocuments(ctx context.Context, db *sql.DB, f ListFilter) ([]DocumentRow, error) {
	q := `SELECT uri, collection, title, modified_at, indexed_at, size_bytes, frontmatter_json
	      FROM documents`
	var where []string
	var args []any
	if f.Collection != "" {
		where = append(where, "collection = ?")
		args = append(args, f.Collection)
	}
	if f.PathPrefix != "" {
		where = append(where, "uri LIKE ? ESCAPE '\\'")
		args = append(args, EscapeLike(f.PathPrefix)+"%")
	}
	if f.FileType != "" {
		where = append(where, "uri LIKE ? ESCAPE '\\'")
		args = append(args, "%."+EscapeLike(f.FileType))
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY uri"
	if f.Limit > 0 {
		q += " LIMIT ?"
		args = append(args, f.Limit)
	}

	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: list documents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []DocumentRow
	for rows.Next() {
		var (
			d                                       DocumentRow
			title, modifiedAt, indexedAt, frontJSON sql.NullString
		)
		if err := rows.Scan(&d.URI, &d.Collection, &title, &modifiedAt, &indexedAt, &d.SizeBytes, &frontJSON); err != nil {
			return nil, fmt.Errorf("storage: scan document row: %w", err)
		}
		d.Title = title.String
		d.ModifiedAt = modifiedAt.String
		d.IndexedAt = indexedAt.String
		d.FrontmatterJSON = frontJSON.String
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate document rows: %w", err)
	}

	return out, nil
}

// EscapeLike escapes the LIKE wildcards (% and _) and the escape character in a
// literal so a path prefix/extension is matched verbatim rather than as a
// pattern. Use it together with `ESCAPE '\'` in a LIKE clause. Shared by the
// document lister and the search filter so both treat user-supplied path/type
// values literally.
func EscapeLike(s string) string {
	return likeEscaper.Replace(s)
}

// likeEscaper is built once at package load so EscapeLike avoids allocating a
// Replacer on every filtered search and document list.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// nullString maps "" to a SQL NULL so optional columns stay null rather than
// holding empty strings.
func nullString(s string) any {
	if s == "" {
		return nil
	}

	return s
}
