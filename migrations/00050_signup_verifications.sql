-- Single-use signup verification tokens, the same shape as
-- password_resets and for the same reason: only the hash is stored,
-- so a database reader cannot verify an account they do not hold the
-- mailbox for.

-- +goose Up
CREATE TABLE signup_verifications (
    id                     UUID PRIMARY KEY,
    user_id                UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    request_ip TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_signup_verifications_user ON signup_verifications(user_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS signup_verifications;
