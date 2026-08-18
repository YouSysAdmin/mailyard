-- Named pools a send can be routed to, and the unit failover happens
-- within.
--
-- Every project gets exactly one group flagged is_default, and every
-- server belongs to exactly one group. That single representation is
-- the point: the alternative, "empty group_id means the default
-- group", leaves two ways to say the same thing and every query has
-- to handle both.

-- +goose Up
CREATE TABLE smtp_server_groups (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    -- Stable handle a send names instead of a uuid, so an integration
    -- does not have to be edited when the servers behind it change.
    -- It is translated to the id once, at accept time, which is the
    -- only place an unknown group can still be a bad request instead
    -- of a queued message that fails later for an invisible reason.
    slug        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- Used when a send names no group. Exactly one per project, held
    -- by the partial unique index below.
    is_default  BOOLEAN NOT NULL DEFAULT FALSE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, slug)
);

-- One default per project, enforced by the database rather than by
-- remembering to clear the old one in every write path.
CREATE UNIQUE INDEX idx_smtp_group_one_default
    ON smtp_server_groups (project_id) WHERE is_default;

-- +goose Down
DROP TABLE IF EXISTS smtp_server_groups;
