-- Bounce reports, attributed to the project that SENT the message.
--
-- Which message a report is about comes from the X-Mailyard-Email-Id
-- header, stamped on every send and returned in the report's original
-- headers. Not the envelope (VERP works only while MAIL FROM is ours)
-- and not the Message-ID (SES rewrites it). Every channel - a DSN
-- arriving at the inbound listener, an SES notification, a relay node
-- giving up - writes through one function, so the rules cannot drift.

-- +goose Up
CREATE TABLE bounces (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    email_id               UUID,
    recipient  TEXT NOT NULL,
    type       TEXT NOT NULL DEFAULT 'hard',
    reason     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_bounces_proj_created ON bounces(project_id, created_at);

-- The keyset page. The id is the tie-break, and a bounce burst writes
-- many rows in the same instant by nature - one provider rejecting a
-- batch produces them all at once.
CREATE INDEX idx_bounces_proj_created_id
    ON bounces (project_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE IF EXISTS bounces;
