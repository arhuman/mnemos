package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/arhuman/mnemos/internal/model"
)

// ReplaceChunks deletes the existing chunks for documentID and inserts the
// provided set within tx. Deleting first lets the FTS triggers cascade-clean
// the external-content index; the inserts then repopulate it. tags and doc_type
// are stored denormalized on each chunk so FTS5 external content stays correct.
func ReplaceChunks(ctx context.Context, tx *sql.Tx, documentID string, chunks []model.Chunk) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE document_id = ?`, documentID); err != nil {
		return fmt.Errorf("storage: delete chunks for %q: %w", documentID, err)
	}

	if len(chunks) == 0 {
		return nil
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO chunks (
			id, document_id, ordinal, heading_path, content,
			tags, doc_type, token_count, start_line, end_line, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("storage: prepare chunk insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()

	for _, c := range chunks {
		_, err := stmt.ExecContext(ctx,
			c.ID, c.DocumentID, c.Ordinal, nullString(c.HeadingPath), c.Content,
			nullString(c.Tags), nullString(c.DocType), c.TokenCount,
			c.StartLine, c.EndLine, nullString(c.MetadataJSON),
		)
		if err != nil {
			return fmt.Errorf("storage: insert chunk %d of %q: %w", c.Ordinal, documentID, err)
		}
	}

	return nil
}
