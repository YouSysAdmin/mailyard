-- Sending domains, and the DKIM key minted when ownership verifies.
--
-- Ownership is answered by GetVerifiedCovering, never by an exact
-- name match: a verified domain COVERS its subdomains, because
-- controlling a zone controls every name under it. Five decisions
-- depend on it - the From domain, DKIM signing, the bounce address,
-- inbound RCPT, and registering a sender address.

-- +goose Up
CREATE TABLE domains (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_by         TEXT NOT NULL DEFAULT '',
    -- Bare lowercase name. Globally unique - the MX listener routes
    -- by recipient domain with no project context. That uniqueness is
    -- also why every ownership check compares the PROJECT: without
    -- it, one tenant could send as a name another had verified.
    domain             TEXT NOT NULL UNIQUE,
    verification_token TEXT NOT NULL,
    verified           BOOLEAN NOT NULL DEFAULT FALSE,
    verified_at        TIMESTAMPTZ,
    -- DKIM signing key, minted when ownership verifies. The private
    -- half is a sealed PEM (the store seals it), never plaintext.
    -- The public half is bare base64 DER and is public by definition.
    dkim_selector      TEXT NOT NULL DEFAULT '',
    dkim_private_key   TEXT NOT NULL DEFAULT '',
    dkim_public_key    TEXT NOT NULL DEFAULT '',
    -- Record checks, separate from `verified`, which means ownership
    -- alone. A domain can be provably yours and still publish no SPF
    -- record, and sending must not wait on that.
    spf_verified       BOOLEAN NOT NULL DEFAULT FALSE,
    dkim_verified      BOOLEAN NOT NULL DEFAULT FALSE,
    dmarc_verified     BOOLEAN NOT NULL DEFAULT FALSE,
    checked_at         TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_domains_proj ON domains(project_id);

-- +goose Down
DROP TABLE IF EXISTS domains;
