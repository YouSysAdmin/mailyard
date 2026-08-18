-- Custom permission groups: a project-defined set of permissions a
-- member can be assigned instead of a role preset.
--
-- The five built-in roles are presets over the permission catalogue
-- and live in code (internal/models/permission), where a release can
-- evolve them. A custom group is the operator's own policy - "support
-- reads mail and writes suppressions, nothing else" - so it is data.
-- The two kinds deliberately do not share a table: presets have no
-- rows to migrate and custom groups have no code to drift from.

-- +goose Up
CREATE TABLE project_groups (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    -- Renameable. The id is the reference project_members carries, so
    -- a rename is cosmetic.
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- JSON array of "resource:action" strings from the permission
    -- catalogue. The wildcard is refused on write - admin-equivalence
    -- is what the admin ROLE is for.
    --
    -- NOT NULL DEFAULT '[]' is load-bearing: the membership join
    -- reads COALESCE(g.permissions, '') and uses the empty string to
    -- mean "no group resolved", so a real group must always yield
    -- non-empty text. '[]' - a group granting nothing - is a valid
    -- deliberate lockdown and must stay distinguishable from absence.
    permissions TEXT NOT NULL DEFAULT '[]',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ,
    UNIQUE (project_id, name)
);

CREATE INDEX idx_project_groups_proj ON project_groups(project_id);

-- +goose Down
DROP TABLE IF EXISTS project_groups;
