-- Delivery attempts against a webhook.
--
-- Grows per EVENT, so the list is keyset paged rather than offset
-- paged and there is no total on it.

-- +goose Up
CREATE TABLE webhook_deliveries (
    id                     UUID PRIMARY KEY,
    webhook_id             UUID NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    project_id             UUID NOT NULL,
    event         TEXT NOT NULL,
    status        TEXT NOT NULL,
    http_status   INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    attempt       INTEGER NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_webhook_deliveries ON webhook_deliveries(webhook_id, created_at);

-- The keyset page. project_id leads so the index also serves the
-- tenancy scope the query filters on first, and the id is the
-- tie-break the cursor needs.
CREATE INDEX idx_webhook_deliveries_keyset
    ON webhook_deliveries (project_id, webhook_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE IF EXISTS webhook_deliveries;
