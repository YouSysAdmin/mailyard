-- Recovery codes: the way back in when the authenticator is gone.
--
-- Ten single-use codes, minted when the second factor is turned on and
-- whenever the owner asks for a fresh set, shown once and stored as
-- SHA-256. A code takes the place of the authenticator code at sign-in
-- and is spent by the same conditional UPDATE the TOTP step uses, so two
-- nodes cannot both honour it. Without these the only recovery was an
-- administrator's reset, which for the sole administrator is a shell.

-- +goose Up
CREATE TABLE user_recovery_codes (
    id         UUID PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash  TEXT NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, code_hash)
);

CREATE INDEX idx_user_recovery_codes_user ON user_recovery_codes(user_id);

-- +goose Down
DROP TABLE IF EXISTS user_recovery_codes;
