-- +goose Up
-- Search on the email log: a recipient address or part of the subject.
--
-- Neither is a prefix, so no btree can serve them. Measured on one week
-- holding 1.19M rows: 1090 ms without an index against 6.2 ms with
-- these, and the scan is linear in the row count, so it stops being
-- usable exactly as an install gets busy. The cost is about a third
-- more disk and 19 us per insert, which is invisible beside an SMTP
-- round trip.
--
-- Pinned to public, and the operator class qualified below. An
-- extension is database-global but its operator classes live in one
-- schema, so an unqualified reference resolves through search_path and
-- the migration then works or not depending on who is connected.
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

-- On the PARENT, so every partition created later inherits them.
-- Per-partition indexes would be one more thing the hourly job has to
-- remember, and forgetting shows up as search being fast for old mail
-- and slow for this week's.
CREATE INDEX idx_emails_subject_trgm ON emails USING gin (subject public.gin_trgm_ops);
CREATE INDEX idx_emails_recipients_trgm ON emails USING gin (recipients public.gin_trgm_ops);

-- +goose Down
DROP INDEX IF EXISTS idx_emails_recipients_trgm;
DROP INDEX IF EXISTS idx_emails_subject_trgm;
