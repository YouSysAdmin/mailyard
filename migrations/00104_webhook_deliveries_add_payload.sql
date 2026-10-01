-- The body each attempt posted, so a delivery can be read back and
-- sent again by hand. Empty on rows written before the column
-- existed, which redeliver refuses rather than guesses at.

-- +goose Up
ALTER TABLE webhook_deliveries ADD COLUMN payload TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE webhook_deliveries DROP COLUMN IF EXISTS payload;
