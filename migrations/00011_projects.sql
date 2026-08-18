-- Projects: the tenant.
--
-- Every tenant table below carries project_id NOT NULL and every
-- store query scopes on it first, so cross-project access looks like
-- a missing resource rather than a refusal.

-- +goose Up
CREATE TABLE projects (
    id                     UUID PRIMARY KEY,
    name             TEXT NOT NULL,
    slug             TEXT NOT NULL UNIQUE,
    description      TEXT NOT NULL DEFAULT '',
    -- Informational pointer, membership rows are the access authority.
    -- Empty for the auth-disabled default project.
    owner_id               UUID,
    default_language TEXT NOT NULL DEFAULT 'en',
    is_personal      BOOLEAN NOT NULL DEFAULT FALSE,
    -- Empty means no explicit assignment: the default plan applies, or
    -- unlimited when none is marked default.
    plan_id                UUID,
    -- When set, every send from this project must use a registered
    -- sender address (senders table).
    strict_senders   BOOLEAN NOT NULL DEFAULT FALSE,

    -- Open and click tracking for ordinary sends. Campaigns track
    -- unconditionally - that is what a campaign is for - so these
    -- govern the API, submission and template paths only. Off by
    -- default because a tracking pixel in a password reset is a bad
    -- look, and nobody should get one by upgrading. A caller may opt
    -- OUT per message and never in: enabling is the project owner's
    -- call.
    track_opens      BOOLEAN NOT NULL DEFAULT FALSE,
    track_clicks     BOOLEAN NOT NULL DEFAULT FALSE,

    -- The envelope sender (SMTP MAIL FROM, the address that becomes
    -- Return-Path at the receiver), used only when the message leaves
    -- through a server this project OWNS. Empty leaves MAIL FROM as
    -- the From address.
    --
    -- It decides where reports GO, and nothing else. It does not
    -- decide which project a report belongs to: that keys on the
    -- X-Mailyard-Email-Id header, which rides inside the message and
    -- survives an envelope a provider rewrote. Attribution by
    -- arrival domain was tried and was broken - a provider forwards
    -- its bounce copy to a mailbox on the PLATFORM's domain, so every
    -- tenant's bounces resolved to the operator and were discarded.
    --
    -- Delivery through the shared platform pool takes
    -- sending.bounce_address instead, because there the sending IPs
    -- really are ours. One installation-wide address for both cases
    -- puts an SPF failure on the operator's domain for every tenant
    -- relay: the receiver checks the return path domain's SPF against
    -- the IP that connected, and a tenant's own relay is not in it.
    bounce_address   TEXT NOT NULL DEFAULT '',

    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ
);

-- +goose Down
DROP TABLE IF EXISTS projects;
