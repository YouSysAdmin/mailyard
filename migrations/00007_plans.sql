-- Plans: the sending and resource caps a project is held to.
--
-- Consumption is counted from the primary tables at check time, so
-- there is no counter state here that could drift from what was
-- actually sent.

-- +goose Up
CREATE TABLE plans (
    id                     UUID PRIMARY KEY,
    name               TEXT NOT NULL UNIQUE,
    description        TEXT NOT NULL DEFAULT '',
    is_default         BOOLEAN NOT NULL DEFAULT FALSE,
    -- Zero means unlimited for every limit column.
    hourly_email_limit BIGINT NOT NULL DEFAULT 0,
    daily_email_limit  BIGINT NOT NULL DEFAULT 0,
    max_api_keys       INTEGER NOT NULL DEFAULT 0,
    max_smtp_servers   INTEGER NOT NULL DEFAULT 0,
    max_domains        INTEGER NOT NULL DEFAULT 0,
    max_subscribers    INTEGER NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ
);

-- +goose Down
DROP TABLE IF EXISTS plans;
