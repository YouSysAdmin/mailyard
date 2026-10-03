-- A key can produce a sandbox capture instead of a queued message, so
-- the ledger names either. Both NULL while the first request runs.

-- +goose Up
ALTER TABLE email_idempotency ADD COLUMN sandbox_email_id UUID;

-- +goose Down
ALTER TABLE email_idempotency DROP COLUMN IF EXISTS sandbox_email_id;
