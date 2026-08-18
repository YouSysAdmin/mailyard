-- The role a project gives to members who carry none of their own.
--
-- Every path that creates a membership can leave role_id empty, which
-- used to fall back to a built-in preset. There are none, so the
-- project names one.
--
-- Empty means those members reach NOTHING, which is deliberate: a
-- project that has not said what its members may do admits them to no
-- resource. Visible in the console and one click to fix, where a
-- baseline nobody chose is neither.
--
-- Resolved in the membership JOIN, so permissions arrive with the
-- caller. No foreign key, like project_members.role_id - deleting a
-- role is refused while the project names it here.

-- +goose Up
ALTER TABLE projects ADD COLUMN default_role_id UUID;

-- +goose Down
ALTER TABLE projects DROP COLUMN IF EXISTS default_role_id;
