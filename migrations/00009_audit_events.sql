-- Operational and security trails.
--
-- One table, two categories: "project" rows carry a project_id and
-- are read by project admins, "security" rows are account activity
-- and carry none.
--
-- No foreign key on actor_id: the trail must survive the account it
-- names being deleted, which is exactly when it matters most. Same
-- reason there is no foreign key on project_id.

-- +goose Up
CREATE TABLE audit_events (
    id                     UUID PRIMARY KEY,
    category    TEXT NOT NULL,
    type        TEXT NOT NULL,
    project_id             UUID,
    actor_id               UUID,
    actor_email TEXT NOT NULL DEFAULT '',
    client_ip   TEXT NOT NULL DEFAULT '',
    method      TEXT NOT NULL DEFAULT '',
    path        TEXT NOT NULL DEFAULT '',
    status      INTEGER NOT NULL DEFAULT 0,
    detail      TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_audit_proj ON audit_events(category, project_id, created_at);
CREATE INDEX idx_audit_actor ON audit_events(category, actor_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS audit_events;
