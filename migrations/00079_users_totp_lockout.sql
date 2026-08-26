-- A second factor that can be guessed is not a second factor.
--
-- TOTP codes were refused one at a time with nothing counting the
-- refusals: the only brake was the per-address login limiter, and an
-- attacker holding the password could pace a guess of the six digits
-- across addresses indefinitely. Five wrong codes now lock the factor
-- for fifteen minutes. Per ACCOUNT, because that is what is being
-- attacked - the address is whatever the attacker chose it to be.

-- +goose Up
ALTER TABLE users
    ADD COLUMN totp_failures     INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN totp_locked_until TIMESTAMPTZ;

-- +goose Down
ALTER TABLE users
    DROP COLUMN IF EXISTS totp_failures,
    DROP COLUMN IF EXISTS totp_locked_until;
