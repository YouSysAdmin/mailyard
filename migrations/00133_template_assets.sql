-- Images uploaded in the template builder. Stored once per project
-- and served publicly at /assets/<public_token>, so a message carries
-- a URL rather than the bytes.
--
-- Bytes live in the blob store (storage_key) or inline as base64
-- (content) when no store is configured. Uploading the same file
-- again answers the row already there, hence the sha256 key.

-- +goose Up
CREATE TABLE template_assets (
    id           UUID PRIMARY KEY,
    project_id   UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    filename     TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size         BIGINT NOT NULL DEFAULT 0,
    sha256       TEXT NOT NULL,
    public_token TEXT NOT NULL UNIQUE,
    storage_key  TEXT NOT NULL DEFAULT '',
    content      TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, sha256)
);

-- +goose Down
DROP TABLE IF EXISTS template_assets;
