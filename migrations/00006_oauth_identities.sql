-- Links an external subject to a local user.
--
-- A subject is only unique WITHIN an issuer, so the key is the pair
-- (provider_id, subject) rather than the subject alone. One user may
-- hold an identity at several providers, hence the second unique key
-- rather than a column on users.

-- +goose Up
CREATE TABLE oauth_identities (
    id                     UUID PRIMARY KEY,
    user_id                UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    provider_id            UUID NOT NULL REFERENCES oauth_providers(id) ON DELETE CASCADE,
    subject       TEXT NOT NULL,
    email         TEXT NOT NULL DEFAULT '',
    name          TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider_id, subject),
    UNIQUE (user_id, provider_id)
);

CREATE INDEX idx_oauth_identities_user ON oauth_identities(user_id);

-- +goose Down
DROP TABLE IF EXISTS oauth_identities;
