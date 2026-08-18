-- SMTP submission logins.
--
-- Separate from api_keys on purpose: a submission credential grants
-- submission and nothing else, and carries no scopes or expiry - it
-- is either usable or revoked.

-- +goose Up
CREATE TABLE smtp_credentials (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_by    TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL,
    -- The AUTH PLAIN username, "smtp_" plus 16 hex characters.
    username      TEXT NOT NULL UNIQUE,
    -- hex(sha256(password)), never the password itself.
    password_hash TEXT NOT NULL,
    -- JSON array.
    allowed_ips   TEXT NOT NULL DEFAULT '[]',

    -- A submission client cannot pass a JSON field, so the pool a
    -- message is routed to binds to the credential it authenticates
    -- with instead.
    smtp_group_id          UUID,

    -- Sandbox routing rides on the CREDENTIAL, never on the message.
    --
    -- That is the whole point of the feature: a developer changes what
    -- their application authenticates with, not what it sends. A field
    -- in the request would eventually be left set to true in
    -- production, or worse, false in staging. The switch is one-way -
    -- a credential marked here is REFUSED if it asks to send for real,
    -- while an ordinary one may opt in per message.
    sandbox       BOOLEAN NOT NULL DEFAULT FALSE,

    revoked       BOOLEAN NOT NULL DEFAULT FALSE,
    last_used_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_smtp_credentials_proj ON smtp_credentials(project_id);

-- +goose Down
DROP TABLE IF EXISTS smtp_credentials;
