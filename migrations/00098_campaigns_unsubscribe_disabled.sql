-- A campaign that carries no List-Unsubscribe headers and no
-- unsubscribe link. An option, never a default: bulk mail without them
-- is filtered by Gmail and Yahoo rather than bounced.

-- +goose Up
ALTER TABLE campaigns ADD COLUMN unsubscribe_disabled BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE campaigns DROP COLUMN IF EXISTS unsubscribe_disabled;
