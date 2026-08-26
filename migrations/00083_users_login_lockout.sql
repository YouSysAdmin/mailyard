-- Per-account lockout on password sign-in, the counterpart of 00079 for
-- the first factor. The per-IP limiter caps one address; a guess spread
-- over many addresses is bounded only by bcrypt until the account itself
-- counts its failures. One row, one count, so every node sees the same
-- number.

-- +goose Up
ALTER TABLE users
    ADD COLUMN login_failures     INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN login_locked_until TIMESTAMPTZ;

-- +goose Down
ALTER TABLE users
    DROP COLUMN IF EXISTS login_failures,
    DROP COLUMN IF EXISTS login_locked_until;
