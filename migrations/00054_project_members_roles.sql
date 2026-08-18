-- The membership row loses its role ladder and gains an owner bit.
--
--   group_id -> role_id   the reference now names a project_roles row
--   role     -> owner     the five-value ladder becomes one boolean
--
-- `role` is renamed to free the word: a role column beside a role_id
-- column is two different things with one name.
--
-- Only owner is translated. admin, editor, viewer and developer become
-- members with no role, reaching nothing until an owner gives them one
-- or names a project default. The presets are gone, and inventing rows
-- to stand in for them would decide a policy that belongs to the
-- project.
--
-- No foreign key on role_id, following the group_id it replaces. The
-- writer holds integrity: SetMemberRole requires the role to exist in
-- the same project, deleting a role is refused while members carry it,
-- and a stale value reads as "no role" rather than locking anybody
-- out.

-- +goose Up
ALTER TABLE project_members RENAME COLUMN group_id TO role_id;
ALTER INDEX idx_project_members_group RENAME TO idx_project_members_role;

ALTER TABLE project_members ADD COLUMN owner BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE project_members SET owner = TRUE WHERE role = 'owner';
ALTER TABLE project_members DROP COLUMN role;

-- Answers "does this project still have an owner", which the member
-- update and remove paths ask before they let the last one go.
CREATE INDEX idx_project_members_owner ON project_members(project_id) WHERE owner;

-- +goose Down
DROP INDEX IF EXISTS idx_project_members_owner;
ALTER TABLE project_members ADD COLUMN role TEXT NOT NULL DEFAULT 'viewer';
UPDATE project_members SET role = 'owner' WHERE owner;
ALTER TABLE project_members DROP COLUMN owner;
ALTER INDEX idx_project_members_role RENAME TO idx_project_members_group;
ALTER TABLE project_members RENAME COLUMN role_id TO group_id;
