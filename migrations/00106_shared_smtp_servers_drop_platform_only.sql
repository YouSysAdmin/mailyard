-- platform_only reserved a pool row for the platform's own mail, which
-- systemmail used to pick and dial itself. Platform mail is now a
-- message of the project platform_mail_project names and goes out
-- through that project's servers, so nothing reads the flag.
--
-- The pick index goes back to the shape 00020 created.

-- +goose Up
DROP INDEX IF EXISTS idx_shared_smtp_pick;
CREATE INDEX idx_shared_smtp_pick ON shared_smtp_servers (status, priority, created_at);
ALTER TABLE shared_smtp_servers DROP COLUMN platform_only;

-- +goose Down
ALTER TABLE shared_smtp_servers ADD COLUMN platform_only BOOLEAN NOT NULL DEFAULT FALSE;
DROP INDEX IF EXISTS idx_shared_smtp_pick;
CREATE INDEX idx_shared_smtp_pick
    ON shared_smtp_servers (status, platform_only, priority, created_at);
