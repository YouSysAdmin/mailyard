-- Single-use password reset tokens.
--
-- Only the hash is stored, so a database reader cannot redeem one.

-- +goose Up
CREATE TABLE password_resets (
    id                     UUID PRIMARY KEY,
    user_id                UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    request_ip TEXT NOT NULL DEFAULT ''
);

CREATE INDEX idx_password_resets_user ON password_resets(user_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS password_resets;
