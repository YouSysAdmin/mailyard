-- Whether a template carries its builder images inside each message as
-- inline cid: parts instead of loading them from the hosted URL.

-- +goose Up
ALTER TABLE templates ADD COLUMN embed_images BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE templates DROP COLUMN IF EXISTS embed_images;
