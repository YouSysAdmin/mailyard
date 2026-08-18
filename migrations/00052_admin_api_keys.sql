-- Platform administration credentials, for /api/v1/admin.
--
-- Their OWN table, following shared_smtp_servers: api_keys carries
-- project_id NOT NULL and every query scopes on it first, so a
-- platform-wide row would break the constraint and the scoping both.
--
-- No permissions column - the catalogue governs what a member may do
-- inside a project, and no resource in it means "may create a user".
--
-- Tokens carry their own marker (mya_ against myk_) so the auth path
-- knows which table to read from the token alone.

-- +goose Up
CREATE TABLE admin_api_keys (
    id                     UUID PRIMARY KEY,
    -- The account that minted it, for the audit trail. Not a foreign
    -- key: deleting the operator who created a deployment credential
    -- must not delete the credential a fleet is authenticating with.
    created_by   TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL,
    -- hex(sha256(token)), never the token itself.
    key_hash     TEXT NOT NULL,
    -- First characters of the token, the lookup index.
    key_prefix   TEXT NOT NULL,
    -- JSON array.
    allowed_ips  TEXT NOT NULL DEFAULT '[]',
    revoked      BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_admin_api_keys_prefix ON admin_api_keys(key_prefix);

-- +goose Down
DROP TABLE IF EXISTS admin_api_keys;
