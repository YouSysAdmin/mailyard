-- Bulk sends to a subscriber list.
--
-- Campaigns track opens and clicks unconditionally, and refuse to
-- send when tracking is unconfigured: bulk mail needs
-- List-Unsubscribe, and Gmail and Yahoo filter rather than bounce
-- without it, so sending anyway fails invisibly.

-- +goose Up
CREATE TABLE campaigns (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_by         TEXT NOT NULL DEFAULT '',
    name               TEXT NOT NULL,
    subject            TEXT NOT NULL DEFAULT '',
    from_email         TEXT NOT NULL,
    from_name          TEXT NOT NULL DEFAULT '',
    template_id            UUID NOT NULL,
    language           TEXT NOT NULL DEFAULT '',
    template_data      TEXT NOT NULL DEFAULT '{}',
    status             TEXT NOT NULL DEFAULT 'draft',
    list_id                UUID NOT NULL,
    send_rate          INTEGER NOT NULL DEFAULT 0,
    send_at_local_time BOOLEAN NOT NULL DEFAULT FALSE,
    ab_test_enabled    BOOLEAN NOT NULL DEFAULT FALSE,
    ab_variants        TEXT NOT NULL DEFAULT '[]',
    -- Which pool the queued messages are routed to. Empty means the
    -- project's default group.
    smtp_group_id          UUID,
    scheduled_at       TIMESTAMPTZ,
    started_at         TIMESTAMPTZ,
    completed_at       TIMESTAMPTZ,
    -- Runner state: when the next batch is due (also the claim lease).
    next_batch_at      TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ
);

CREATE INDEX idx_campaigns_proj ON campaigns(project_id);
-- The runner's claim query.
CREATE INDEX idx_campaigns_due ON campaigns(status, next_batch_at);

-- +goose Down
DROP TABLE IF EXISTS campaigns;
