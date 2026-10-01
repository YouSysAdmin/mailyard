-- A campaign sent without the sender address's S/MIME or PGP
-- signature, the same opt-out a single send carries as
-- disable_signing. Only ever off: whether an address signs is decided
-- where its key is.

-- +goose Up
ALTER TABLE campaigns ADD COLUMN disable_signing BOOLEAN NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE campaigns DROP COLUMN IF EXISTS disable_signing;
