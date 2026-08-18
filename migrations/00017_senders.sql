-- Approved sender addresses.
--
-- Rows can only be created for domains the project has verified, and
-- they feed the From selectors in the console. A project can turn on
-- strict_senders to make them mandatory on every send.

-- +goose Up
CREATE TABLE senders (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_by TEXT NOT NULL DEFAULT '',
    email      TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, email)
);

-- +goose Down
DROP TABLE IF EXISTS senders;
