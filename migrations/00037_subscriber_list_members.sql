-- Static list membership.

-- +goose Up
CREATE TABLE subscriber_list_members (
    id                     UUID PRIMARY KEY,
    list_id                UUID NOT NULL REFERENCES subscriber_lists(id) ON DELETE CASCADE,
    subscriber_id          UUID NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (list_id, subscriber_id)
);

-- +goose Down
DROP TABLE IF EXISTS subscriber_list_members;
