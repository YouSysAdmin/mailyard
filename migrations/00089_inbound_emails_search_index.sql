-- +goose Up
-- Search on the inbound log: part of the envelope sender or of any
-- recipient, case-insensitive.
--
-- A substring is not a prefix, so no btree serves it, and this table
-- has no ceiling when retention is off. The sandbox gets no index: a
-- plan and a retention window bound it, and it is the table written
-- in bulk.
--
-- The operator class is qualified for the reason in
-- 00062_emails_search_index.sql, which also created the extension.
CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public;

CREATE INDEX idx_inbound_sender_trgm ON inbound_emails USING gin (sender public.gin_trgm_ops);
CREATE INDEX idx_inbound_recipients_trgm ON inbound_emails USING gin (recipients public.gin_trgm_ops);

-- +goose Down
DROP INDEX IF EXISTS idx_inbound_recipients_trgm;
DROP INDEX IF EXISTS idx_inbound_sender_trgm;
