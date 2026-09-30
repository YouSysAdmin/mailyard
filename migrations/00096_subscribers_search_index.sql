-- +goose Up
-- Search on the audience: part of an address or a name.
--
-- The console's subscriber picker and the list page both ask
-- `LOWER(email) LIKE '%q%'` per keystroke, and neither is a prefix, so
-- the only index on the table (project_id) left every search a scan of
-- the whole project. Trigram indexes on the LOWERED expressions serve
-- exactly the predicate the store writes.
--
-- Pinned to public, same as 00062: the extension is database-global
-- but its operator class lives in one schema.
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

CREATE INDEX idx_subscribers_email_trgm ON subscribers USING gin (LOWER(email) public.gin_trgm_ops);
CREATE INDEX idx_subscribers_name_trgm ON subscribers USING gin (LOWER(name) public.gin_trgm_ops);

-- +goose Down
DROP INDEX IF EXISTS idx_subscribers_name_trgm;
DROP INDEX IF EXISTS idx_subscribers_email_trgm;
