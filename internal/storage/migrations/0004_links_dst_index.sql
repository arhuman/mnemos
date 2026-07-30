-- +goose Up
-- +goose StatementBegin
-- Index inbound link lookups (who links to X). Without it, backlink and
-- neighbor-inbound queries scan the whole links table; links.dst_doc is a plain
-- uri string, so the index is on the raw value joined to documents.uri.
CREATE INDEX idx_links_dst_doc ON links(dst_doc);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_links_dst_doc;
-- +goose StatementEnd
