-- A project's own SMTP servers.
--
-- Owning ANY server - even a disabled one, even one that refuses the
-- sender - is the act of taking delivery over: the shared platform
-- pool is consulted only when a project owns none.

-- +goose Up
CREATE TABLE smtp_servers (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_by       TEXT NOT NULL DEFAULT '',
    name             TEXT NOT NULL,
    host             TEXT NOT NULL,
    port             INTEGER NOT NULL,
    username         TEXT NOT NULL DEFAULT '',
    -- Sealed at rest by core/crypto. Empty in, empty out - an SMTP
    -- server with no password is ordinary.
    password         TEXT NOT NULL DEFAULT '',
    encryption       TEXT NOT NULL DEFAULT 'none',
    -- JSON array of exact addresses or *@domain wildcards.
    allowed_emails   TEXT NOT NULL DEFAULT '[]',

    -- Which pool this server sits in, and where it sits within it.
    -- Lowest priority first, created_at breaks ties so the order is
    -- total and failover is deterministic.
    group_id               UUID,
    priority         INTEGER NOT NULL DEFAULT 0,

    -- Some providers (Amazon SES being the common one) rewrite
    -- Message-ID and Date after accepting a message and then DKIM-sign
    -- the result themselves. Both headers are in Mailyard's signed set -
    -- as they must be - so a signature applied here is guaranteed to
    -- arrive broken. A broken signature is ignored rather than
    -- punished (RFC 6376), but it is pure noise, and this flag turns
    -- it off for servers where the provider's own signing is what
    -- actually carries DMARC. It is re-evaluated per candidate during
    -- failover, since that is the whole reason it exists.
    skip_dkim        BOOLEAN NOT NULL DEFAULT FALSE,

    -- Which SNS topic this server's SES feedback arrives on.
    --
    -- Per server rather than an installation-wide config list, for
    -- two reasons. SES is a property of ONE server - a tenant
    -- configures their own SES account as their own row here - so a
    -- platform config key could only ever serve an operator who owned
    -- the account themselves. And an installation-wide allowlist was
    -- disconnected from attribution: the project a bounce belongs to
    -- comes from the email row, so ANY allowlisted topic could report
    -- about ANY project's message. Binding the topic to a server
    -- binds it to a project.
    ses_topic_arn    TEXT NOT NULL DEFAULT '',

    status           TEXT NOT NULL DEFAULT 'enabled',
    validation_error TEXT NOT NULL DEFAULT '',
    validated_at     TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_smtp_servers_proj ON smtp_servers(project_id);
CREATE INDEX idx_smtp_servers_group ON smtp_servers (project_id, group_id, priority, created_at);

-- The allowlist lookup, which runs on a public endpoint.
CREATE INDEX idx_smtp_servers_ses_topic
    ON smtp_servers (ses_topic_arn) WHERE ses_topic_arn <> '';

-- +goose Down
DROP TABLE IF EXISTS smtp_servers;
