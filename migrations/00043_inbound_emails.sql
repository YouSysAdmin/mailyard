-- Mail that arrived at the MX listener.
--
-- Recipients are gated at RCPT time on domains verified by DNS, so a
-- row here already belongs to a known project.

-- +goose Up
CREATE TABLE inbound_emails (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    domain_id              UUID,
    message_id    TEXT NOT NULL DEFAULT '',
    dedup_hash    TEXT NOT NULL DEFAULT '',
    sender        TEXT NOT NULL DEFAULT '',
    -- JSON array of envelope recipients.
    recipients    TEXT NOT NULL DEFAULT '[]',
    subject       TEXT NOT NULL DEFAULT '',
    text_body     TEXT NOT NULL DEFAULT '',
    html_body     TEXT NOT NULL DEFAULT '',
    -- JSON object of parsed headers.
    headers       TEXT NOT NULL DEFAULT '{}',
    -- JSON array of attachments, inline or pointing at the blob store.
    attachments   TEXT NOT NULL DEFAULT '[]',
    -- The SPF/DKIM/DMARC verdict as it stood when the message arrived.
    -- Stored rather than derived on read, because the checks depend on
    -- the IP that connected and on DNS at that moment, neither of
    -- which can be recovered later. A message that passed DMARC in
    -- March is still a message that passed in March, even if the
    -- sender's record has changed since. Empty means never checked,
    -- which is NOT a failure - the read side keeps the distinction by
    -- leaving Auth nil.
    auth          TEXT NOT NULL DEFAULT '',
    -- Raw wire bytes, kept only when parsing failed.
    raw           BYTEA,
    size          BIGINT NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'received',
    error_message TEXT NOT NULL DEFAULT '',
    received_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_inbound_proj_received ON inbound_emails(project_id, received_at DESC);
CREATE INDEX idx_inbound_message_id ON inbound_emails(project_id, message_id);
CREATE INDEX idx_inbound_dedup ON inbound_emails(project_id, dedup_hash);

-- +goose Down
DROP TABLE IF EXISTS inbound_emails;
