-- +goose Up
-- +goose StatementBegin
-- The journal tier (ADR-0011): session exchanges are indexed like any other
-- document but excluded from ranked retrieval, so transcripts never compete with
-- citable knowledge in a result set.
--
-- The marker is a stored column rather than a uri prefix on purpose. A path check
-- ("uri LIKE 'journal/%'") breaks silently on a rename, an `mnemos mv` of the
-- subtree, or a second journal root, and it fails in the dangerous direction: a
-- journal that stops being recognised starts appearing in cited results.
--
-- Defaulting to 0 is what makes this migration safe on a populated database:
-- every existing row is non-journal, which is true, so no backfill is needed and
-- retrieval behaviour is unchanged until something is explicitly stamped.
ALTER TABLE documents ADD COLUMN journal INTEGER NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose StatementBegin
-- Partial index: the journal is the minority tier, and every query that cares is
-- either "exclude journal rows" (served by the default-0 majority) or "list the
-- journal" (served by this). Indexing only the 1s keeps it small.
CREATE INDEX idx_documents_journal ON documents(journal) WHERE journal = 1;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_documents_journal;
-- +goose StatementEnd

-- +goose StatementBegin
ALTER TABLE documents DROP COLUMN journal;
-- +goose StatementEnd
