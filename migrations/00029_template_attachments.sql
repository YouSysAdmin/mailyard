-- Files attached to a template, appended to every send that uses it
-- and to every campaign built on it.
--
-- Bytes live in the blob store (storage_key) or inline as base64
-- (content) when no store is configured.

-- +goose Up
CREATE TABLE template_attachments (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    template_id            UUID NOT NULL REFERENCES templates(id) ON DELETE CASCADE,
    filename     TEXT NOT NULL,
    content_type TEXT NOT NULL DEFAULT '',
    size         BIGINT NOT NULL DEFAULT 0,
    storage_key  TEXT NOT NULL DEFAULT '',
    content      TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_tmpl_att ON template_attachments(project_id, template_id);

-- +goose Down
DROP TABLE IF EXISTS template_attachments;
