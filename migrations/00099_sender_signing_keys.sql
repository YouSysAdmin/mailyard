-- The key a sender address signs its mail with, S/MIME or OpenPGP.
--
-- One row per sender, in a table of its own rather than columns on
-- senders: the two kinds carry different material, and the private
-- half is ONE sealed column for the rekey command to rewrite. The
-- public half is in the clear, like a DKIM public key or a
-- certificate's cert_pem.
--
-- project_id is repeated from the sender so every query here names
-- the tenant without a join, which is what the tenancy guard checks.

-- +goose Up
CREATE TABLE sender_signing_keys (
    sender_id   UUID PRIMARY KEY REFERENCES senders(id) ON DELETE CASCADE,
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL,
    private_key TEXT NOT NULL,
    public_key  TEXT NOT NULL,
    fingerprint TEXT NOT NULL,
    algorithm   TEXT NOT NULL DEFAULT '',
    subject     TEXT NOT NULL DEFAULT '',
    issuer      TEXT NOT NULL DEFAULT '',
    not_after   TIMESTAMPTZ,
    sign        BOOLEAN NOT NULL DEFAULT true,
    attach_key  BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Serves the expiry sweep, which asks for every key running out
-- before a date across all projects.
CREATE INDEX idx_sender_signing_keys_not_after ON sender_signing_keys(not_after)
    WHERE not_after IS NOT NULL;

-- +goose Down
DROP TABLE IF EXISTS sender_signing_keys;
