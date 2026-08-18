-- A member can carry a custom permission group.
--
-- Empty means the role preset applies, which is every existing row
-- and every row the ordinary paths create. Non-empty REPLACES the
-- role's permission set - the role itself stays on the row and keeps
-- governing the owner-only acts and the admin-only destructive
-- deletes, which permissions deliberately cannot express.
--
-- No foreign key: the empty string is not a group. The writer holds
-- integrity instead - SetMemberGroup is the only code that writes this
-- column and requires the group to exist in the same project, and
-- deleting a group is refused while members reference it. A stale
-- value is a race window, not a state, and the reader falls back.

-- +goose Up
ALTER TABLE project_members ADD COLUMN group_id UUID;

-- Serves the roster join and the referenced-delete count.
CREATE INDEX idx_project_members_group ON project_members(project_id, group_id);

-- +goose Down
DROP INDEX IF EXISTS idx_project_members_group;
ALTER TABLE project_members DROP COLUMN IF EXISTS group_id;
