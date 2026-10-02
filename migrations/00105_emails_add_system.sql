-- Platform mail sent through a project's pipeline. A system row uses
-- the project's servers, relay nodes and DKIM and nothing else of the
-- project's: it is not counted in email_volume, not filtered by its
-- suppressions, not tracked, not fed to its webhooks or contacts, and
-- not answered by any project-scoped reader. Its body is cleared by
-- the same statement that finalizes it, because a password reset
-- link is a credential.

-- +goose Up
ALTER TABLE emails ADD COLUMN system BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE emails DROP COLUMN IF EXISTS system;
