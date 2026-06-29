-- +goose Up
-- +goose StatementBegin
CREATE TABLE embeddings (
    chunk_id TEXT PRIMARY KEY,
    model TEXT NOT NULL,
    dim INTEGER NOT NULL,
    vector BLOB NOT NULL,
    FOREIGN KEY(chunk_id) REFERENCES chunks(id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_embeddings_model ON embeddings(model);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_embeddings_model;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS embeddings;
-- +goose StatementEnd
