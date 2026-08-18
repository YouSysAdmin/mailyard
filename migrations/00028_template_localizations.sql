-- The actual subject and body, per language, for one template
-- version.

-- +goose Up
CREATE TABLE template_localizations (
    id                     UUID PRIMARY KEY,
    version_id             UUID NOT NULL REFERENCES template_versions(id) ON DELETE CASCADE,
    language         TEXT NOT NULL,
    subject_template TEXT NOT NULL,
    html_template    TEXT NOT NULL DEFAULT '',
    text_template    TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ,
    UNIQUE (version_id, language)
);

CREATE INDEX idx_template_localizations_ver ON template_localizations(version_id);

-- +goose Down
DROP TABLE IF EXISTS template_localizations;
