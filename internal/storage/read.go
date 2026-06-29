package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/arhuman/mnemos/internal/model"
)

// GetDocumentByURI returns the document with the given uri, or (nil, nil) when
// no such document exists. It is a read-only accessor used by the MCP read tool.
func GetDocumentByURI(ctx context.Context, db *sql.DB, uri string) (*model.Document, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, uri, collection, content_hash, title, mime_type,
		       size_bytes, modified_at, indexed_at, frontmatter_json
		FROM documents WHERE uri = ?
	`, uri)

	d, err := scanDocument(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil //nolint:nilnil // documented not-found result: callers and tests treat (nil, nil) as "no such row"
	case err != nil:
		return nil, fmt.Errorf("storage: get document %q: %w", uri, err)
	default:
		return d, nil
	}
}

// GetDocumentByID returns the document with the given id, or (nil, nil) when no
// such document exists. It is a read-only accessor used by the MCP read tool to
// resolve a chunk's owning document.
func GetDocumentByID(ctx context.Context, db *sql.DB, id string) (*model.Document, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, uri, collection, content_hash, title, mime_type,
		       size_bytes, modified_at, indexed_at, frontmatter_json
		FROM documents WHERE id = ?
	`, id)

	d, err := scanDocument(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil //nolint:nilnil // documented not-found result: callers and tests treat (nil, nil) as "no such row"
	case err != nil:
		return nil, fmt.Errorf("storage: get document by id %q: %w", id, err)
	default:
		return d, nil
	}
}

// GetChunkByID returns the chunk with the given id, or (nil, nil) when no such
// chunk exists. It is a read-only accessor used by the MCP read tool.
func GetChunkByID(ctx context.Context, db *sql.DB, id string) (*model.Chunk, error) {
	row := db.QueryRowContext(ctx, `
		SELECT id, document_id, ordinal, heading_path, content,
		       tags, doc_type, token_count, start_line, end_line, metadata_json
		FROM chunks WHERE id = ?
	`, id)

	c, err := scanChunk(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil //nolint:nilnil // documented not-found result: callers and tests treat (nil, nil) as "no such row"
	case err != nil:
		return nil, fmt.Errorf("storage: get chunk %q: %w", id, err)
	default:
		return c, nil
	}
}

// GetChunkContentsByIDs fetches the content of many chunks in a single query,
// returning a map from chunk id to content. Missing ids are simply absent from
// the map. An empty ids slice short-circuits to an empty map with no query. It
// is the batch accessor used by mnemos.context to avoid a per-result round-trip
// (the N+1 the single-id GetChunkByID would cause in a loop). The id count is
// bounded by the search limit, well under SQLite's bound-parameter cap.
func GetChunkContentsByIDs(ctx context.Context, db *sql.DB, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT id, content FROM chunks WHERE id IN (%s)`,
		strings.Join(placeholders, ","),
	)

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: get chunk contents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var id, content string
		if err := rows.Scan(&id, &content); err != nil {
			return nil, fmt.Errorf("storage: scan chunk content: %w", err)
		}
		out[id] = content
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate chunk contents: %w", err)
	}

	return out, nil
}

// GetChunksByDocURI returns every chunk owned by the document with the given
// uri, ordered by ordinal. The chunks table is the source of truth for document
// content (the file on disk may have moved); reconstructing a document means
// reassembling these chunks. An empty slice means the uri matched no chunks.
func GetChunksByDocURI(ctx context.Context, db *sql.DB, uri string) ([]model.Chunk, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT c.id, c.document_id, c.ordinal, c.heading_path, c.content,
		       c.tags, c.doc_type, c.token_count, c.start_line, c.end_line, c.metadata_json
		FROM chunks c
		JOIN documents d ON d.id = c.document_id
		WHERE d.uri = ?
		ORDER BY c.ordinal
	`, uri)
	if err != nil {
		return nil, fmt.Errorf("storage: get chunks for %q: %w", uri, err)
	}
	defer func() { _ = rows.Close() }()

	var chunks []model.Chunk
	for rows.Next() {
		c, err := scanChunk(rows)
		if err != nil {
			return nil, fmt.Errorf("storage: scan chunk for %q: %w", uri, err)
		}
		chunks = append(chunks, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate chunks for %q: %w", uri, err)
	}

	return chunks, nil
}

// scanner abstracts *sql.Row and *sql.Rows so the scan helpers serve both the
// single-row and multi-row accessors.
type scanner interface {
	Scan(dest ...any) error
}

// scanDocument reads one documents row, mapping NULL optional columns to "".
func scanDocument(s scanner) (*model.Document, error) {
	var (
		d           model.Document
		title       sql.NullString
		mimeType    sql.NullString
		sizeBytes   sql.NullInt64
		modifiedAt  sql.NullString
		indexedAt   sql.NullString
		frontmatter sql.NullString
	)
	if err := s.Scan(
		&d.ID, &d.URI, &d.Collection, &d.ContentHash, &title, &mimeType,
		&sizeBytes, &modifiedAt, &indexedAt, &frontmatter,
	); err != nil {
		return nil, err
	}
	d.Title = title.String
	d.MimeType = mimeType.String
	d.SizeBytes = sizeBytes.Int64
	d.ModifiedAt = modifiedAt.String
	d.IndexedAt = indexedAt.String
	d.FrontmatterJSON = frontmatter.String

	return &d, nil
}

// scanChunk reads one chunks row, mapping NULL optional columns to "".
func scanChunk(s scanner) (*model.Chunk, error) {
	var (
		c           model.Chunk
		headingPath sql.NullString
		tags        sql.NullString
		docType     sql.NullString
		metadata    sql.NullString
	)
	if err := s.Scan(
		&c.ID, &c.DocumentID, &c.Ordinal, &headingPath, &c.Content,
		&tags, &docType, &c.TokenCount, &c.StartLine, &c.EndLine, &metadata,
	); err != nil {
		return nil, err
	}
	c.HeadingPath = headingPath.String
	c.Tags = tags.String
	c.DocType = docType.String
	c.MetadataJSON = metadata.String

	return &c, nil
}
