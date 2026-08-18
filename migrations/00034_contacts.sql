-- Addresses this project has actually delivered to, with their
-- tallies.
--
-- Written by the delivery worker as each message reaches a terminal
-- state, never by a user - the API is read-only and the store has no
-- Put.
--
-- Suppression is deliberately NOT a column here. It is resolved at
-- read time from the suppressions table so the flag can never
-- disagree with the list that governs sending.

-- +goose Up
CREATE TABLE contacts (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    email          TEXT NOT NULL,
    name           TEXT NOT NULL DEFAULT '',
    sent_count     INTEGER NOT NULL DEFAULT 0,
    fail_count     INTEGER NOT NULL DEFAULT 0,
    last_sent_at   TIMESTAMPTZ,
    last_failed_at TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, email)
);

-- Serves the default listing order (most recently active first).
CREATE INDEX idx_contacts_proj_activity ON contacts(project_id, last_sent_at);

-- +goose Down
DROP TABLE IF EXISTS contacts;
