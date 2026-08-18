-- In-app notifications, addressed to a project rather than a person.
--
-- What they report is a fact about the project, and read state is
-- shared so one member clearing an alert clears it for everyone.

-- +goose Up
CREATE TABLE notifications (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    type       TEXT NOT NULL,
    severity   TEXT NOT NULL DEFAULT 'info',
    title      TEXT NOT NULL,
    body       TEXT NOT NULL DEFAULT '',
    link       TEXT NOT NULL DEFAULT '',
    -- Collapses repeats. A job that runs every fifteen minutes must
    -- not file the same alert on every run, so it writes a key naming
    -- the condition and its window and the unique index does the rest.
    dedupe_key TEXT NOT NULL DEFAULT '',
    read_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Serves the list and the unread badge, which are the only two reads.
CREATE INDEX idx_notifications_proj_created ON notifications(project_id, created_at);

-- Partial so the many rows with no dedupe key do not collide with
-- each other.
CREATE UNIQUE INDEX idx_notifications_dedupe
    ON notifications(project_id, dedupe_key)
    WHERE dedupe_key <> '';

-- +goose Down
DROP TABLE IF EXISTS notifications;
