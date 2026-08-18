-- One row per recipient of a campaign.
--
-- email_id links to the queued message, which is what the finalize
-- hook syncs status back through.

-- +goose Up
CREATE TABLE campaign_messages (
    id                     UUID PRIMARY KEY,
    campaign_id            UUID NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    subscriber_id          UUID NOT NULL,
    email_id               UUID,
    status        TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT NOT NULL DEFAULT '',
    variant       TEXT NOT NULL DEFAULT '',
    -- Per-recipient due time (send-at-local-time campaigns).
    deliver_at    TIMESTAMPTZ,
    sent_at       TIMESTAMPTZ,
    opened_at     TIMESTAMPTZ,
    clicked_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (campaign_id, subscriber_id)
);

-- The batch query.
CREATE INDEX idx_campaign_messages_pending ON campaign_messages(campaign_id, status, deliver_at);
-- The finalize-hook sync by email id.
CREATE INDEX idx_campaign_messages_email ON campaign_messages(email_id);

-- +goose Down
DROP TABLE IF EXISTS campaign_messages;
