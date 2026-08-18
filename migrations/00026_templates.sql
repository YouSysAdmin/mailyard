-- Templates: the named thing a send asks for.
--
-- The content is not here. A template holds versions, a version holds
-- localizations, and active_version_id says which one a send renders -
-- so editing a template never changes what is going out until the new
-- version is made active.

-- +goose Up
CREATE TABLE templates (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name              TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    default_language  TEXT NOT NULL DEFAULT 'en',
    active_version_id      UUID,
    sample_data       TEXT NOT NULL DEFAULT '',
    created_by        TEXT NOT NULL DEFAULT '',
    last_edited_by    TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ,
    UNIQUE (project_id, name)
);

CREATE INDEX idx_templates_proj ON templates(project_id);

-- +goose Down
DROP TABLE IF EXISTS templates;
