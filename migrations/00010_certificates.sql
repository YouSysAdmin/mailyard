-- One place for every certificate the installation holds, instead of
-- a directory on whichever node happened to mint it.
--
-- The relay CA needs it: a CA key in a file on one api node does not
-- survive that node and cannot be reached by the others. The same
-- table fixes something older - autocert caches to a local directory,
-- so N nodes each order their own certificate and Let's Encrypt allows
-- five duplicates a week, meaning the sixth node gets none.
--
-- scope says what kind of thing this is, name distinguishes entries
-- within it (a hostname, an autocert cache key, or empty).

-- +goose Up
CREATE TABLE certificates (
    scope      TEXT NOT NULL,
    name       TEXT NOT NULL,

    -- Sealed by core/crypto, always, without exception.
    --
    -- Everything stored here either IS a private key or contains one:
    -- an autocert cache entry is the key and the chain concatenated,
    -- a CA entry is the key. One rule for the whole column beats a
    -- rule that depends on the scope, because the second kind is the
    -- kind somebody eventually gets wrong.
    data       TEXT NOT NULL,

    -- The public certificate in the clear, so the console can show an
    -- expiry and the node list can show a fingerprint without the
    -- encryption key being involved at all. Empty where the entry has
    -- no meaningful public half on its own.
    cert_pem   TEXT NOT NULL DEFAULT '',

    -- Parsed out of cert_pem on write, best effort. Drives the
    -- expiry warnings - a peer pair that expires takes every
    -- handshake down at once, so it must never be a surprise.
    not_after  TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,

    PRIMARY KEY (scope, name)
);

-- The expiry sweep and the console both want "what is about to die",
-- across scopes.
CREATE INDEX idx_certificates_expiry ON certificates (not_after);

-- +goose Down
DROP TABLE IF EXISTS certificates;
