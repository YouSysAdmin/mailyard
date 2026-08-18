-- Shared CSS a template version can point at.

-- +goose Up
CREATE TABLE stylesheets (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    css        TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ
);

CREATE INDEX idx_stylesheets_proj ON stylesheets(project_id);

-- +goose Down
DROP TABLE IF EXISTS stylesheets;
