-- Platform-owned SMTP servers: the fallback pool for projects that
-- have configured none of their own.
--
-- A separate table and not a nullable project_id on smtp_servers: that
-- column is NOT NULL with a foreign key, and every tenant query scopes
-- on it first, so one forgotten scope clause would leak a server
-- between projects. Here a project-scoped query cannot reach these
-- rows at all - a project gets delivery through the pool, never sight
-- of it.

-- +goose Up
CREATE TABLE shared_smtp_servers (
    id                     UUID PRIMARY KEY,
    created_by       TEXT NOT NULL DEFAULT '',
    name             TEXT NOT NULL,
    host             TEXT NOT NULL,
    port             INTEGER NOT NULL,
    username         TEXT NOT NULL DEFAULT '',
    -- Sealed at rest by core/crypto, exactly like the per-project
    -- table.
    password         TEXT NOT NULL DEFAULT '',
    encryption       TEXT NOT NULL DEFAULT 'none',
    skip_dkim        BOOLEAN NOT NULL DEFAULT FALSE,
    -- JSON array of exact addresses or *@domain wildcards.
    allowed_emails   TEXT NOT NULL DEFAULT '[]',
    -- JSON array of bare sender domains. Narrower than allowed_emails
    -- and applied in addition to it.
    allowed_domains  TEXT NOT NULL DEFAULT '[]',
    -- permissive: any sender the domain rules admit.
    -- strict: the SENDING project must also have verified the
    -- sender's domain, which is what stops one tenant relaying as
    -- another's.
    security_mode    TEXT NOT NULL DEFAULT 'permissive',
    -- Lowest first, created_at breaks ties so the order is total.
    priority         INTEGER NOT NULL DEFAULT 0,
    -- Which SNS topic this server's SES feedback arrives on. See the
    -- same column on smtp_servers for why it lives per server.
    ses_topic_arn    TEXT NOT NULL DEFAULT '',
    -- A relay node that has enrolled but not been approved sits in
    -- 'pending' here. No separate approval column: status already
    -- gates delivery and ListEnabled already filters on it, so the
    -- mechanism that was already there does the job rather than a
    -- second one that has to agree with it.
    status           TEXT NOT NULL DEFAULT 'enabled',
    validation_error TEXT NOT NULL DEFAULT '',
    validated_at     TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_shared_smtp_pick ON shared_smtp_servers (status, priority, created_at);

-- The allowlist lookup, which runs on a public endpoint.
CREATE INDEX idx_shared_smtp_ses_topic
    ON shared_smtp_servers (ses_topic_arn) WHERE ses_topic_arn <> '';

-- +goose Down
DROP TABLE IF EXISTS shared_smtp_servers;
