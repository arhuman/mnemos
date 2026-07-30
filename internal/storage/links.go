package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/arhuman/mnemos/internal/model"
)

// ReplaceLinks deletes the outbound edges for srcDoc and inserts the provided
// set within tx. dst_doc is a plain URI string with no foreign key, so link
// targets need not be ingested yet.
func ReplaceLinks(ctx context.Context, tx *sql.Tx, srcDoc string, links []model.Link) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM links WHERE src_doc = ?`, srcDoc); err != nil {
		return fmt.Errorf("storage: delete links for %q: %w", srcDoc, err)
	}

	if len(links) == 0 {
		return nil
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO links (src_doc, dst_doc) VALUES (?, ?)`)
	if err != nil {
		return fmt.Errorf("storage: prepare link insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, l := range links {
		if _, err := stmt.ExecContext(ctx, l.SrcDoc, l.DstDoc); err != nil {
			return fmt.Errorf("storage: insert link %q->%q: %w", l.SrcDoc, l.DstDoc, err)
		}
	}

	return nil
}

// Neighbor is one 1-hop link-graph neighbor of a document: the connected
// document plus the edge direction that reached it. For an outbound edge
// Resolved is false when the target uri has no ingested document (a dangling
// link), in which case Title and Collection are empty. Inbound neighbors are
// always resolved, because the source of a link is a stored document.
type Neighbor struct {
	URI        string
	Title      string
	Collection string
	Direction  string
	Resolved   bool
}

// Direction values for a Neighbor.
const (
	DirOutbound = "outbound"
	DirInbound  = "inbound"
)

// OutboundNeighbors returns the documents srcURI links to (its outbound edges),
// left-joined to documents so a dangling target still surfaces with Resolved
// false and empty title/collection. Ordered by target uri for stable output. An
// unknown srcURI simply has no outbound edges and yields an empty slice.
func OutboundNeighbors(ctx context.Context, db *sql.DB, srcURI string) ([]Neighbor, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT l.dst_doc, dst.title, dst.collection
		FROM links l
		JOIN documents src ON l.src_doc = src.id
		LEFT JOIN documents dst ON dst.uri = l.dst_doc
		WHERE src.uri = ?
		ORDER BY l.dst_doc`, srcURI)
	if err != nil {
		return nil, fmt.Errorf("storage: outbound neighbors for %q: %w", srcURI, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Neighbor
	for rows.Next() {
		var (
			n                 Neighbor
			title, collection sql.NullString
		)
		if err := rows.Scan(&n.URI, &title, &collection); err != nil {
			return nil, fmt.Errorf("storage: scan outbound neighbor: %w", err)
		}
		// documents.collection is NOT NULL, so a NULL here means the LEFT JOIN
		// missed: the target uri is not an ingested document (dangling link).
		n.Direction = DirOutbound
		n.Resolved = collection.Valid
		n.Title = title.String
		n.Collection = collection.String
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate outbound neighbors: %w", err)
	}

	return out, nil
}

// InboundNeighbors returns the documents that link to dstURI (its backlinks),
// joined to their source document for title/collection. Always Resolved (a link
// source is a stored document). Ordered by source uri. Relies on
// idx_links_dst_doc (migration 0004) to avoid a full links scan.
func InboundNeighbors(ctx context.Context, db *sql.DB, dstURI string) ([]Neighbor, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT src.uri, src.title, src.collection
		FROM links l
		JOIN documents src ON l.src_doc = src.id
		WHERE l.dst_doc = ?
		ORDER BY src.uri`, dstURI)
	if err != nil {
		return nil, fmt.Errorf("storage: inbound neighbors for %q: %w", dstURI, err)
	}
	defer func() { _ = rows.Close() }()

	var out []Neighbor
	for rows.Next() {
		var (
			n     Neighbor
			title sql.NullString
		)
		if err := rows.Scan(&n.URI, &title, &n.Collection); err != nil {
			return nil, fmt.Errorf("storage: scan inbound neighbor: %w", err)
		}
		n.Direction = DirInbound
		n.Resolved = true
		n.Title = title.String
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate inbound neighbors: %w", err)
	}

	return out, nil
}
