-- Audiences a campaign sends to: static membership or a filter.

-- +goose Up
CREATE TABLE subscriber_lists (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    type         TEXT NOT NULL DEFAULT 'static',
    -- JSON array of filter rules, dynamic lists only.
    filter_rules TEXT NOT NULL DEFAULT '[]',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ
);

CREATE INDEX idx_subscriber_lists_proj ON subscriber_lists(project_id);

-- +goose Down
DROP TABLE IF EXISTS subscriber_lists;
