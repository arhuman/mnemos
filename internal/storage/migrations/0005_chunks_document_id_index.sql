-- +goose Up
-- +goose StatementBegin
-- Index the chunks -> documents foreign key. SQLite does not index a foreign key
-- automatically, so without this every documents/chunks join (bm25 search, vector
-- hydrate, similar, doctor) and every `DELETE FROM chunks WHERE document_id = ?`
-- scans the whole chunks table. The delete path is the worst of these: deleting a
-- document relies on ON DELETE CASCADE, so with foreign_keys=ON SQLite scans
-- chunks on every forget, move, watcher eviction, and re-ingest of a changed file.
CREATE INDEX idx_chunks_document_id ON chunks(document_id);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_chunks_document_id;
-- +goose StatementEnd
