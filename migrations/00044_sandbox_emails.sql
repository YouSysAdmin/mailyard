-- The email sandbox: messages a developer submitted that Mailyard
-- captured instead of delivering.
--
-- Its own table and not a flag on emails. Sandbox mail is throwaway -
-- a CI run writes ten thousand rows in a morning - where emails is the
-- delivery log an operator reasons about. One table would put that
-- traffic in every count, dashboard, retention decision and index on
-- the delivery path, each needing a "not the fake ones" clause.

-- +goose Up
CREATE TABLE sandbox_emails (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,

    -- Which door it came in by, and the credential that opened it.
    -- A developer chasing "why did nothing arrive" needs to know
    -- which of their two integrations actually sent this.
    source        TEXT NOT NULL DEFAULT '',
    credential_id          UUID,
    api_key_id             UUID,

    -- The SMTP envelope. Kept apart from the header addresses because
    -- they routinely differ, and a developer debugging a Bcc or a
    -- VERP return path is asking about exactly that difference.
    sender     TEXT NOT NULL DEFAULT '',
    recipients TEXT NOT NULL DEFAULT '[]',

    subject   TEXT NOT NULL DEFAULT '',
    text_body TEXT NOT NULL DEFAULT '',
    html_body TEXT NOT NULL DEFAULT '',
    headers   TEXT NOT NULL DEFAULT '{}',

    -- Attachment METADATA only: filename, content type, size. The
    -- bytes are not copied here - raw below already holds the whole
    -- message, and a second copy would double the storage of the one
    -- table most likely to be written in bulk. A download re-parses
    -- raw, which costs a parse and keeps deletion to one statement
    -- with no blob store to orphan.
    attachments TEXT NOT NULL DEFAULT '[]',

    -- The wire bytes, ALWAYS - not only on a parse failure like
    -- inbound_emails. Seeing the raw message with its headers is half
    -- of what a sandbox is for, and a parsed view cannot be turned
    -- back into one.
    raw  BYTEA,
    size BIGINT NOT NULL DEFAULT 0,

    client_ip TEXT NOT NULL DEFAULT '',

    -- When retention may remove this row. Decided at capture from the
    -- platform default and the per-message override, and stored rather
    -- than recomputed at sweep time so the console can say "expires in
    -- 3 days" and mean it. NULL is keep forever.
    --
    -- The consequence is deliberate: changing the platform default
    -- governs new mail and leaves what is already here alone. A
    -- setting change should not silently destroy a message somebody is
    -- looking at.
    expires_at TIMESTAMPTZ,

    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sandbox_proj_received ON sandbox_emails(project_id, received_at DESC);
CREATE INDEX idx_sandbox_expires ON sandbox_emails(expires_at) WHERE expires_at IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS sandbox_emails;
