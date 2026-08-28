-- A campaign from a no-reply mailbox needs somewhere for a reader's
-- answer to go. One address, the Reply-To header, empty for none.

-- +goose Up
ALTER TABLE campaigns
    ADD COLUMN reply_to TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE campaigns
    DROP COLUMN IF EXISTS reply_to;
