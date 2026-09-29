-- The console pages a campaign's messages in fan-out order with a
-- keyset on (created_at, id), which this index serves.

-- +goose Up
CREATE INDEX idx_campaign_messages_order ON campaign_messages (campaign_id, created_at, id);

-- +goose Down
DROP INDEX IF EXISTS idx_campaign_messages_order;
