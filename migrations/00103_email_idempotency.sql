-- One row per Idempotency-Key a project has sent with, so a client
-- that retries a send after a timeout gets the message it already
-- queued rather than a second one. email_id is NULL while the first
-- request is still running, which is how a concurrent duplicate is
-- told apart from a replay. Pruned after a day by the retention sweep,
-- not governed by a setting: a retry window longer than that is not a
-- retry.
--
-- Its own table rather than a column on emails, because emails is
-- partitioned by created_at and a unique index there would have to
-- include the partition key, which is exactly what a key sent twice
-- across midnight must not be allowed to differ on.

-- +goose Up
CREATE TABLE email_idempotency (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    email_id   UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, key)
);
CREATE INDEX idx_email_idempotency_created ON email_idempotency (created_at);

-- +goose Down
DROP TABLE IF EXISTS email_idempotency;
