-- When every message of a sent campaign reached a terminal state.
--
-- Set once, by whichever caller sees the last message settle first,
-- and that caller alone emits campaign.completed. The runner's own
-- completed_at comes earlier, while the last batch is still queued.

-- +goose Up
ALTER TABLE campaigns ADD COLUMN settled_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE campaigns DROP COLUMN IF EXISTS settled_at;
