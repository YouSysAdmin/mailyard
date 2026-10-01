-- Tags and metadata the caller attaches to a message so it can be
-- found again by the caller's own ids: a list of labels to filter the
-- log by, and a flat string map carried through to the record. Both
-- are JSON in TEXT columns like the rest of the row.
--
-- The tags index is an expression over the jsonb cast, which lets
-- `tags::jsonb @> '["x"]'` be served without the column itself being
-- jsonb. On the partitioned parent, so every partition inherits it.

-- +goose Up
ALTER TABLE emails ADD COLUMN tags TEXT NOT NULL DEFAULT '[]';
ALTER TABLE emails ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}';
CREATE INDEX idx_emails_tags ON emails USING gin ((tags::jsonb));

-- +goose Down
DROP INDEX IF EXISTS idx_emails_tags;
ALTER TABLE emails DROP COLUMN IF EXISTS metadata;
ALTER TABLE emails DROP COLUMN IF EXISTS tags;
