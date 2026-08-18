-- Individual opens and clicks.
--
-- Events point at the EMAIL. campaign_message_id is filled only when
-- that email belongs to a campaign, so campaign reporting still works
-- while transactional tracking exists at all.

-- +goose Up
CREATE TABLE tracking_events (
    id                     UUID PRIMARY KEY,
    email_id               UUID,
    campaign_message_id    UUID,
    event_type          TEXT NOT NULL,
    tracked_link_id        UUID,
    ip                  TEXT NOT NULL DEFAULT '',
    user_agent          TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tracking_events_msg ON tracking_events(campaign_message_id, created_at);
CREATE INDEX idx_tracking_events_email ON tracking_events (email_id, event_type);

-- +goose Down
DROP TABLE IF EXISTS tracking_events;
