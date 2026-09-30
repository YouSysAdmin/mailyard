-- Custom headers a campaign puts on every message it sends, as a JSON
-- object. Laid over the project's defaults and under nothing: a
-- campaign is the message.

-- +goose Up
ALTER TABLE campaigns ADD COLUMN headers_json TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE campaigns DROP COLUMN IF EXISTS headers_json;
