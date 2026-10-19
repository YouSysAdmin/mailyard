-- Every invitation a project has issued, one row each, kept apart
-- from project_invitations because cancelling an invitation deletes
-- its row there. The invitation budget counts these, so an invitation
-- cancelled and issued again is still a second mail. Pruned by the
-- retention sweep once no window can read a row.

-- +goose Up
CREATE TABLE project_invitation_sends (
    id         UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    email      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_project_invitation_sends_project ON project_invitation_sends(project_id, created_at);
CREATE INDEX idx_project_invitation_sends_created ON project_invitation_sends(created_at);

-- Backfill the window the budget reads, or an upgrade hands every
-- project a fresh hour.
INSERT INTO project_invitation_sends (id, project_id, email, created_at)
SELECT id, project_id, email, created_at
FROM project_invitations
WHERE created_at >= now() - INTERVAL '1 hour';

-- +goose Down
DROP TABLE IF EXISTS project_invitation_sends;
