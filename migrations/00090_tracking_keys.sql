-- Tracking keys a rekey retired. Each one still verifies the
-- unsubscribe links minted under it, which never expire, and nothing
-- else. Sealed under the current encryption key like every other
-- secret column, so the next rekey reseals them.

-- +goose Up
CREATE TABLE tracking_keys (
    id         UUID PRIMARY KEY,
    key        TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS tracking_keys;
