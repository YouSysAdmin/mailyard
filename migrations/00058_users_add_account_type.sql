-- +goose Up
-- Two questions about an account that had three answers between them.
--
-- HOW IT SIGNS IN is stored now, not inferred from an empty
-- password_hash inside the passkey handler. The console cannot see the
-- hash, so the profile page told everybody to ask an administrator -
-- including local accounts an admin had just created, who could then
-- change nothing about their own login.
--
-- 1 local, 2 OIDC. DEFAULT 1 is right for every row that has a
-- password, and the ones without are corrected below.
ALTER TABLE users ADD COLUMN account_type INTEGER NOT NULL DEFAULT 1;
UPDATE users SET account_type = 2 WHERE password_hash IS NULL OR password_hash = '';

-- WHETHER IT ADMINISTERS THE INSTALLATION had two columns, role and
-- super_user, and nothing anywhere told them apart: IsAdmin() ORed
-- them and the console ORed them again. One boolean instead.
ALTER TABLE users ADD COLUMN admin BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE users SET admin = TRUE WHERE super_user = TRUE OR role = 'admin';

ALTER TABLE users DROP COLUMN role;
ALTER TABLE users DROP COLUMN super_user;

-- +goose Down
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'user';
ALTER TABLE users ADD COLUMN super_user BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE users SET role = 'admin' WHERE admin = TRUE;
ALTER TABLE users DROP COLUMN admin;
ALTER TABLE users DROP COLUMN account_type;
