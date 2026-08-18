-- Signup verification state.
--
-- TRUE by default: every account that exists before this migration,
-- and every account created by a trusted path afterwards (admin,
-- invitation, OIDC, bootstrap), is considered verified. Only public
-- self-registration on an install with system mail creates a FALSE
-- row, and confirming the emailed link flips it.

-- +goose Up
ALTER TABLE users ADD COLUMN email_verified BOOLEAN NOT NULL DEFAULT TRUE;

-- +goose Down
ALTER TABLE users DROP COLUMN IF EXISTS email_verified;
