-- Pending invitations to join a project.
--
-- The token is the credential and is stored in the clear, unlike a
-- password reset: an invitation grants a role in one project, it is
-- addressed to a mailbox that has to accept it, and the link is
-- handed back to the inviter to copy when system mail is off. There
-- is nothing to redeem it into without also controlling that address.

-- +goose Up
CREATE TABLE project_invitations (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    email      TEXT NOT NULL,
    role       TEXT NOT NULL DEFAULT 'viewer',
    token      TEXT NOT NULL UNIQUE,
    status     TEXT NOT NULL DEFAULT 'pending',
    invited_by TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_project_invitations_proj ON project_invitations(project_id);

-- +goose Down
DROP TABLE IF EXISTS project_invitations;
