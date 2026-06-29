-- +goose Up
-- +goose StatementBegin
CREATE TABLE documents (
    id TEXT PRIMARY KEY,
    uri TEXT NOT NULL,
    collection TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    title TEXT,
    mime_type TEXT,
    size_bytes INTEGER,
    modified_at TEXT,
    indexed_at TEXT,
    frontmatter_json TEXT
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE UNIQUE INDEX idx_documents_uri ON documents(uri);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE chunks (
    id TEXT PRIMARY KEY,
    document_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL,
    heading_path TEXT,
    content TEXT NOT NULL,
    tags TEXT,
    doc_type TEXT,
    token_count INTEGER,
    start_line INTEGER,
    end_line INTEGER,
    metadata_json TEXT,
    FOREIGN KEY(document_id) REFERENCES documents(id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE VIRTUAL TABLE chunks_fts USING fts5(
    content,
    heading_path,
    tags,
    doc_type,
    content='chunks',
    content_rowid='rowid'
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER chunks_ai AFTER INSERT ON chunks BEGIN
    INSERT INTO chunks_fts(rowid, content, heading_path, tags, doc_type)
    VALUES (new.rowid, new.content, new.heading_path, new.tags, new.doc_type);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER chunks_ad AFTER DELETE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, content, heading_path, tags, doc_type)
    VALUES ('delete', old.rowid, old.content, old.heading_path, old.tags, old.doc_type);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TRIGGER chunks_au AFTER UPDATE ON chunks BEGIN
    INSERT INTO chunks_fts(chunks_fts, rowid, content, heading_path, tags, doc_type)
    VALUES ('delete', old.rowid, old.content, old.heading_path, old.tags, old.doc_type);
    INSERT INTO chunks_fts(rowid, content, heading_path, tags, doc_type)
    VALUES (new.rowid, new.content, new.heading_path, new.tags, new.doc_type);
END;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE links (
    src_doc TEXT NOT NULL,
    dst_doc TEXT NOT NULL,
    FOREIGN KEY(src_doc) REFERENCES documents(id) ON DELETE CASCADE
);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE INDEX idx_links_src_doc ON links(src_doc);
-- +goose StatementEnd

-- +goose StatementBegin
CREATE TABLE events (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at TEXT NOT NULL
);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS events;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX IF EXISTS idx_links_src_doc;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS links;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER IF EXISTS chunks_au;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER IF EXISTS chunks_ad;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TRIGGER IF EXISTS chunks_ai;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS chunks_fts;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS chunks;
-- +goose StatementEnd

-- +goose StatementBegin
DROP INDEX IF EXISTS idx_documents_uri;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE IF EXISTS documents;
-- +goose StatementEnd
