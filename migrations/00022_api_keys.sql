-- Machine API credentials, for /api/v1.
--
-- Bound to one project and carrying scopes. Only the hash is stored,
-- and the prefix is what a lookup indexes on.

-- +goose Up
CREATE TABLE api_keys (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_by   TEXT NOT NULL DEFAULT '',
    name         TEXT NOT NULL,
    -- hex(sha256(token)), never the token itself.
    key_hash     TEXT NOT NULL,
    -- First characters of the token, the lookup index.
    key_prefix   TEXT NOT NULL,
    -- JSON arrays.
    scopes       TEXT NOT NULL DEFAULT '["send"]',
    allowed_ips  TEXT NOT NULL DEFAULT '[]',
    -- Everything this key sends is CAPTURED into the sandbox instead
    -- of delivered. See smtp_credentials.sandbox for why the decision
    -- lives on the credential and never in the request.
    sandbox      BOOLEAN NOT NULL DEFAULT FALSE,
    revoked      BOOLEAN NOT NULL DEFAULT FALSE,
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_api_keys_prefix ON api_keys(key_prefix);
CREATE INDEX idx_api_keys_proj ON api_keys(project_id);

-- +goose Down
DROP TABLE IF EXISTS api_keys;
