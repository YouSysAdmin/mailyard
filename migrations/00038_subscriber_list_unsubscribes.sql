-- Per-list opt-outs.
--
-- A row of its own rather than removing the membership, so a dynamic
-- list whose filter still matches the person does not quietly put
-- them back.

-- +goose Up
CREATE TABLE subscriber_list_unsubscribes (
    id                     UUID PRIMARY KEY,
    list_id                UUID NOT NULL REFERENCES subscriber_lists(id) ON DELETE CASCADE,
    subscriber_id          UUID NOT NULL REFERENCES subscribers(id) ON DELETE CASCADE,
    reason          TEXT NOT NULL DEFAULT '',
    unsubscribed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (list_id, subscriber_id)
);

-- +goose Down
DROP TABLE IF EXISTS subscriber_list_unsubscribes;
