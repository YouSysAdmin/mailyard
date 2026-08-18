-- One editable revision of a template.
--
-- ON DELETE SET NULL on the stylesheet, not CASCADE: deleting shared
-- CSS must not take the templates that referenced it.

-- +goose Up
CREATE TABLE template_versions (
    id                     UUID PRIMARY KEY,
    template_id            UUID NOT NULL REFERENCES templates(id) ON DELETE CASCADE,
    version       INTEGER NOT NULL,
    stylesheet_id          UUID REFERENCES stylesheets(id) ON DELETE SET NULL,
    sample_data   TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (template_id, version)
);

CREATE INDEX idx_template_versions_tpl ON template_versions(template_id);

-- +goose Down
DROP TABLE IF EXISTS template_versions;
