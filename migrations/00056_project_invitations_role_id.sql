-- An invitation offers a project role, not one of five built-in ones.
--
-- Empty offers the project's default role, which is also where an
-- invitation lands if the role it named is deleted first. The link
-- stays redeemable rather than dying for a reason the recipient
-- cannot see.
--
-- Pending invitations keep their row and lose their offer, so they now
-- offer the default. Re-issue any that meant something narrower.

-- +goose Up
ALTER TABLE project_invitations ADD COLUMN role_id UUID;
ALTER TABLE project_invitations DROP COLUMN role;

-- +goose Down
ALTER TABLE project_invitations ADD COLUMN role TEXT NOT NULL DEFAULT 'viewer';
ALTER TABLE project_invitations DROP COLUMN IF EXISTS role_id;
