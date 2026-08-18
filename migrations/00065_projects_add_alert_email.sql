-- Where a project's alerts go, beside its owners.
--
-- Owners are the audience by construction - several are allowed and no
-- list has to be kept in step with the membership. This column covers
-- the rest: a ticket queue or a shared ops mailbox.
--
-- ADDITIVE. Setting it does not stop the owners hearing, or an install
-- could have every warning quietly redirected to an address nobody
-- reads while the people accountable never learn of it.
--
-- Empty means owners only.

-- +goose Up
ALTER TABLE projects ADD COLUMN alert_email TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE projects DROP COLUMN IF EXISTS alert_email;
