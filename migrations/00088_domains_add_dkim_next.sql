-- A DKIM key rotation in progress: the next selector and keypair,
-- published beside the current record and cut over by verify once
-- the new record is seen. Empty when no rotation is pending.

-- +goose Up
ALTER TABLE domains
    ADD COLUMN dkim_next_selector    TEXT NOT NULL DEFAULT '',
    ADD COLUMN dkim_next_private_key TEXT NOT NULL DEFAULT '',
    ADD COLUMN dkim_next_public_key  TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE domains
    DROP COLUMN dkim_next_selector,
    DROP COLUMN dkim_next_private_key,
    DROP COLUMN dkim_next_public_key;
