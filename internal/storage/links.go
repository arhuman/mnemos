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
