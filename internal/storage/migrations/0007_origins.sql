-- +goose Up
-- +goose StatementBegin
-- Registered external origins (ADR-0013): a canonical tree indexed in place,
-- read-only, without copying it under kb/.
--
-- The registration lives here rather than in mnemos.toml because a path is a
-- location, and the config carries behaviour only (ADR-0005). The cost is that
-- the database stops being purely reconstructible from kb/, which `origin list
-- --json` mitigates: a lost database costs a reindex, not a lost configuration.
--
-- prefix is the URI namespace: a document from this origin is stored as
-- "<prefix>/<path relative to the origin root>". It is the primary key because
-- the namespace, not the path, is what document URIs depend on. Re-registering a
-- moved tree under the same prefix keeps every URI stable.
--
-- path is stored already resolved (symlinks evaluated at registration), so a
-- later swap of an intermediate symlink cannot silently redirect an origin.
--
-- collection is the fallback label for documents that do not declare a
-- `collection:` frontmatter, matching how --collection behaves everywhere else.
--
-- There is deliberately no foreign key from documents to origins: a document's
-- membership is derived from its uri prefix, so dropping an origin never
-- cascades away content the operator may still want to inspect. `origin remove`
-- evicts explicitly instead.
CREATE TABLE origins (
    prefix        TEXT PRIMARY KEY,
    path          TEXT NOT NULL,
    collection    TEXT NOT NULL,
    registered_at TEXT NOT NULL,
    last_indexed_at TEXT
);
-- +goose StatementEnd

-- +goose StatementBegin
-- Registering the same tree under two prefixes would index it twice under two
-- namespaces, doubling storage and returning the same content as two distinct
-- citations. One prefix per path is the invariant.
CREATE UNIQUE INDEX idx_origins_path ON origins(path);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_origins_path;
-- +goose StatementEnd

-- +goose StatementBegin
DROP TABLE origins;
-- +goose StatementEnd
