-- Campaign audience members.
--
-- Distinct from contacts, which the worker writes from what was
-- actually delivered. A subscriber is somebody a person put on a
-- list.

-- +goose Up
CREATE TABLE subscribers (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    email           TEXT NOT NULL,
    name            TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'subscribed',
    -- Flat JSON object merged into campaign template data.
    custom_fields   TEXT NOT NULL DEFAULT '{}',
    timezone        TEXT NOT NULL DEFAULT '',
    language        TEXT NOT NULL DEFAULT '',
    subscribed_at   TIMESTAMPTZ,
    unsubscribed_at TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ,
    UNIQUE (project_id, email)
);

CREATE INDEX idx_subscribers_proj ON subscribers(project_id);

-- +goose Down
DROP TABLE IF EXISTS subscribers;
