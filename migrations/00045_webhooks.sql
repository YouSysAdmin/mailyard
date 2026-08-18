-- Outgoing webhook endpoints.

-- +goose Up
CREATE TABLE webhooks (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_by TEXT NOT NULL DEFAULT '',
    url        TEXT NOT NULL,
    -- JSON arrays: subscribed event names and sender filters.
    events     TEXT NOT NULL DEFAULT '[]',
    filters    TEXT NOT NULL DEFAULT '[]',
    -- HMAC signing secret, returned once at create time.
    secret     TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_webhooks_proj ON webhooks(project_id);

-- +goose Down
DROP TABLE IF EXISTS webhooks;
