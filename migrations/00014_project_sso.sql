-- Per-project SSO policy.
--
-- At most one row per project, hence the primary key on project_id
-- rather than a surrogate id.
--
-- ON DELETE CASCADE from oauth_providers is deliberate: deleting a
-- provider drops the policies that name it, which fails OPEN (the
-- project stops requiring SSO) rather than closed (nobody can get in
-- and the row points at a provider that no longer exists).

-- +goose Up
CREATE TABLE project_sso (
    project_id             UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    provider_id            UUID NOT NULL REFERENCES oauth_providers(id) ON DELETE CASCADE,
    enforce_sso     BOOLEAN NOT NULL DEFAULT FALSE,
    auto_provision  BOOLEAN NOT NULL DEFAULT FALSE,
    -- Role given to an auto-provisioned member. Least privilege by
    -- default: joining through SSO should not hand out edit rights.
    default_role    TEXT NOT NULL DEFAULT 'viewer',
    allowed_domains TEXT NOT NULL DEFAULT '[]',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Serves the auto-provision sweep, which asks "which projects want
-- members from this provider?" on every SSO sign-in.
CREATE INDEX idx_project_sso_provider ON project_sso(provider_id, auto_provision);

-- +goose Down
DROP TABLE IF EXISTS project_sso;
