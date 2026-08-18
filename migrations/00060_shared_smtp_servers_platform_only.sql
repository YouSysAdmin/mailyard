-- +goose Up
-- The platform's own mail - invitations, password resets, signup
-- confirmations - now leaves through the shared pool instead of a
-- second SMTP server configured in yaml under system_mail.
--
-- One pool and one place to configure it, so platform credentials are
-- not set up twice.
--
-- platform_only reserves a row for that traffic: resolveShared skips
-- it and systemmail prefers it. FALSE is right for a small install
-- with one server carrying both.
ALTER TABLE shared_smtp_servers ADD COLUMN platform_only BOOLEAN NOT NULL DEFAULT FALSE;

-- The tenant pick reads (status, priority, created_at) and now also
-- has to exclude the reserved rows.
DROP INDEX IF EXISTS idx_shared_smtp_pick;
CREATE INDEX idx_shared_smtp_pick
    ON shared_smtp_servers (status, platform_only, priority, created_at);

-- +goose Down
DROP INDEX IF EXISTS idx_shared_smtp_pick;
CREATE INDEX idx_shared_smtp_pick ON shared_smtp_servers (status, priority, created_at);
ALTER TABLE shared_smtp_servers DROP COLUMN platform_only;
