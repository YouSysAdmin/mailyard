-- Which signature the message was accepted to carry: '' for none,
-- or the kind of the sender's key at accept time. The decision is
-- stamped here so the log can say what went out, the key itself is
-- read at delivery so a replaced key applies to what is still queued.

-- +goose Up
ALTER TABLE emails ADD COLUMN signing TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE emails DROP COLUMN IF EXISTS signing;
