package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// AppendEvent inserts one events row within tx. id must be unique (the pipeline
// derives it from the document id and timestamp); documentID ties the event to
// its document so it is cascaded away when the document is deleted (empty maps
// to NULL for document-less events); payloadJSON is opaque JSON; createdAt is
// RFC3339.
func AppendEvent(ctx context.Context, tx *sql.Tx, id, documentID, eventType, payloadJSON, createdAt string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO events (id, document_id, type, payload_json, created_at) VALUES (?, ?, ?, ?, ?)`,
		id, nullString(documentID), eventType, payloadJSON, createdAt,
	)
	if err != nil {
		return fmt.Errorf("storage: append event %q: %w", eventType, err)
	}

	return nil
}
