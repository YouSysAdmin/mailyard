-- The tag filter is case-insensitive like every filter in the API, so
-- the index serves the lowered tags. lower() over the JSON text keeps
-- it a valid array, so the containment test still applies.

-- +goose Up
DROP INDEX IF EXISTS idx_emails_tags;
CREATE INDEX idx_emails_tags_lower ON emails USING gin ((lower(tags)::jsonb));

-- +goose Down
DROP INDEX IF EXISTS idx_emails_tags_lower;
CREATE INDEX idx_emails_tags ON emails USING gin ((tags::jsonb));
