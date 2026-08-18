-- Runtime-configured identity providers.
--
-- Platform level on purpose: a user account is a platform entity that
-- then holds project memberships, so "who may sign in" is not a
-- tenant decision.
--
-- client_secret is stored sealed by the crypto service, same as
-- smtp_servers.password.

-- +goose Up
CREATE TABLE oauth_providers (
    id                     UUID PRIMARY KEY,
    name                   TEXT NOT NULL,
    slug                   TEXT NOT NULL UNIQUE,
    type                   TEXT NOT NULL DEFAULT 'oidc',
    client_id              TEXT NOT NULL DEFAULT '',
    client_secret          TEXT NOT NULL DEFAULT '',
    issuer                 TEXT NOT NULL DEFAULT '',
    auth_url               TEXT NOT NULL DEFAULT '',
    token_url              TEXT NOT NULL DEFAULT '',
    userinfo_url           TEXT NOT NULL DEFAULT '',
    scopes                 TEXT NOT NULL DEFAULT '[]',
    enabled                BOOLEAN NOT NULL DEFAULT TRUE,
    hidden                 BOOLEAN NOT NULL DEFAULT FALSE,
    auto_register          BOOLEAN NOT NULL DEFAULT TRUE,
    require_email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    allowed_domains        TEXT NOT NULL DEFAULT '[]',
    allowed_emails         TEXT NOT NULL DEFAULT '[]',
    groups_claim           TEXT NOT NULL DEFAULT '',
    allowed_groups         TEXT NOT NULL DEFAULT '[]',
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Serves the login page's "which providers can I offer" query.
CREATE INDEX idx_oauth_providers_enabled ON oauth_providers(enabled, hidden);

-- +goose Down
DROP TABLE IF EXISTS oauth_providers;
