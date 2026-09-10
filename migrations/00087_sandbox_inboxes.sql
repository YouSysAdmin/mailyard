-- Sandbox INBOXES: a name over a list of sender addresses.
--
-- An inbox is a saved filter over sandbox_emails.sender, decided when
-- the list is READ. Nothing on the capture row points at an inbox, so
-- editing one reclassifies every capture already held and deleting one
-- deletes no mail. Addresses are stored lowercased and trimmed so the
-- list query compares lower(sender) against them directly.

-- +goose Up
CREATE TABLE sandbox_inboxes (
    id          UUID PRIMARY KEY,
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    addresses   TEXT NOT NULL DEFAULT '[]',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ,
    UNIQUE (project_id, name)
);

-- +goose Down
DROP TABLE IF EXISTS sandbox_inboxes;
