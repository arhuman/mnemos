-- +goose Up
-- Tie events to their document so a document delete cascades its events away,
-- instead of leaving orphaned audit rows. The column is nullable: events
-- written before this migration keep document_id = NULL (never cascaded).
-- +goose StatementBegin
ALTER TABLE events ADD COLUMN document_id TEXT REFERENCES documents(id) ON DELETE CASCADE;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX IF NOT EXISTS idx_events_document_id ON events(document_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_events_document_id;
-- +goose StatementEnd

-- +goose StatementBegin
-- ALTER TABLE ... DROP COLUMN requires SQLite 3.35.0+.
-- Satisfied by modernc.org/sqlite v1.53.0 (embeds SQLite 3.49.x). If the
-- SQLite floor is ever lowered, replace this with a table-copy workaround.
ALTER TABLE events DROP COLUMN document_id;
-- +goose StatementEnd
