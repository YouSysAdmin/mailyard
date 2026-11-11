-- The invitation send log also records who invited, so the budget can
-- count per inviting account and per address across every project.
-- A project delete keeps its rows: the projects an account owns are
-- not a budget it can reset. The retention sweep still prunes them.

-- +goose Up
ALTER TABLE project_invitation_sends ADD COLUMN invited_by UUID;

UPDATE project_invitation_sends s
SET invited_by = NULLIF(i.invited_by, '')::uuid
FROM project_invitations i
WHERE i.id = s.id;

ALTER TABLE project_invitation_sends ALTER COLUMN project_id DROP NOT NULL;
ALTER TABLE project_invitation_sends DROP CONSTRAINT project_invitation_sends_project_id_fkey;
ALTER TABLE project_invitation_sends ADD CONSTRAINT project_invitation_sends_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE SET NULL;

-- +goose Down
DELETE FROM project_invitation_sends WHERE project_id IS NULL;
ALTER TABLE project_invitation_sends DROP CONSTRAINT project_invitation_sends_project_id_fkey;
ALTER TABLE project_invitation_sends ADD CONSTRAINT project_invitation_sends_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE;
ALTER TABLE project_invitation_sends ALTER COLUMN project_id SET NOT NULL;
ALTER TABLE project_invitation_sends DROP COLUMN invited_by;
