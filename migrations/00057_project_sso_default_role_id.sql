-- Auto-provisioning names a project role too.
--
-- The column it replaces defaulted to 'viewer' for least privilege.
-- The intent survives, the mechanism cannot - there is no
-- least-privileged role in the binary any more.
--
-- Empty means the provisioned member carries no role and picks up the
-- project default, like anybody else added without one. So signing in
-- through an IdP grants what the project grants its members, never
-- more.

-- +goose Up
ALTER TABLE project_sso ADD COLUMN default_role_id UUID;
ALTER TABLE project_sso DROP COLUMN default_role;

-- +goose Down
ALTER TABLE project_sso ADD COLUMN default_role TEXT NOT NULL DEFAULT 'viewer';
ALTER TABLE project_sso DROP COLUMN IF EXISTS default_role_id;
