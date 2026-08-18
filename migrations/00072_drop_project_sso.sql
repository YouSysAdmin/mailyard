-- A project no longer pins an identity provider.
--
-- project_sso let a PROJECT decide how a person signed in, which the
-- request path itself says is not a project's business: "a project can
-- only govern access to its own data, not the sign-in itself"
-- (enforceProjectSSO, internal/server/middleware_project.go). The table
-- was that contradiction made durable, and it cost three things.
--
-- The enforcement was BYPASSABLE. It ran only against the project named
-- in the X-Mailyard-Project-Id header, while the path-addressed surface
-- (/api/v1/projects/:id/members, /roles, /invitations, /sso, rename,
-- delete) resolves access on membership alone. And stampProject returned
-- early when no header arrived and the caller belonged to more than one
-- project, so enforcement was skipped outright. An owner whose project
-- mandated an IdP could sign in with a PASSWORD, send no header, and
-- rewrite or delete that very policy.
--
-- It WEDGED THE CONSOLE. machineAuth stamps the project for the whole
-- /api/v1 group, so a stale enforcing project id in localStorage made
-- even GET /projects answer 403 - leaving the switcher empty, the
-- no-project card unrendered, and the id in place, because it is only
-- cleared on 401.
--
-- And it was UNSATISFIABLE for a multi-project member: a session records
-- one auth_provider_id and this table allowed one provider per project,
-- so belonging to two projects with different IdPs could never work.
--
-- What replaces it, all of it already present: a provider admits people
-- by allowed_domains / allowed_emails / require_email_verified, which is
-- the right layer and the answer to a global IdP like Google;
-- auto_register creates the account; and membership comes from an
-- INVITATION, which is also where the role comes from - so a group in an
-- IdP is never a privilege grant. An installation that wants SSO only
-- sets auth.local.enabled=false, which refuses password login outright.
--
-- Dropped rather than left unused: the operator confirmed no
-- installation has any SSO configured, so every project_sso row
-- everywhere is absent and this removes a feature nobody had switched
-- on rather than changing behaviour under anyone.
--
-- sessions.auth_provider_id STAYS. Its only reader was the enforcement
-- being removed here, but it records how a session was started, which is
-- worth showing on the sessions page - see the model comment.
--
-- No Down that restores the data, because there is none to restore. The
-- table comes back empty, which is what it was.

-- +goose Up
DROP TABLE IF EXISTS project_sso;

-- +goose Down
CREATE TABLE project_sso (
    project_id      UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    provider_id     UUID NOT NULL REFERENCES oauth_providers(id) ON DELETE CASCADE,
    enforce_sso     BOOLEAN NOT NULL DEFAULT FALSE,
    auto_provision  BOOLEAN NOT NULL DEFAULT FALSE,
    default_role_id UUID,
    allowed_domains TEXT NOT NULL DEFAULT '[]',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_project_sso_provider ON project_sso(provider_id, auto_provision);
