-- When the recipient of this message unsubscribed through its link,
-- beside opened_at and clicked_at. The durable record of a campaign
-- unsubscribe: the tracking event is swept by retention, this is not.

-- +goose Up
ALTER TABLE campaign_messages ADD COLUMN unsubscribed_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE campaign_messages DROP COLUMN IF EXISTS unsubscribed_at;
