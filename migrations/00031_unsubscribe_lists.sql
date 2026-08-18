-- Transactional opt-out SCOPES.
--
-- No membership table: a list is a named scope, and "opted out" is a
-- suppression row pointing at it. Deleting a list keeps those rows -
-- an opt-out is a statement the person made, not a property of the
-- list.

-- +goose Up
CREATE TABLE unsubscribe_lists (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    public_name TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    active      BOOLEAN NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ,
    UNIQUE (project_id, name)
);

-- +goose Down
DROP TABLE IF EXISTS unsubscribe_lists;
