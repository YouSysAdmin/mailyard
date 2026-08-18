-- Console accounts.
--
-- Platform level, not tenant level: an account is a person who signs
-- in, and project membership is a separate row. That is what lets one
-- person belong to several projects with a different role in each.

-- +goose Up
CREATE TABLE users (
    id                     UUID PRIMARY KEY,
    email          TEXT NOT NULL UNIQUE,
    password_hash  TEXT,
    role           TEXT NOT NULL DEFAULT 'admin',
    super_user     BOOLEAN NOT NULL DEFAULT FALSE,
    disabled       BOOLEAN NOT NULL DEFAULT FALSE,
    -- TOTP two-factor. The secret is sealed by core/crypto before it
    -- arrives and the account is only marked enabled once the user
    -- proves a valid code.
    totp_secret    TEXT NOT NULL DEFAULT '',
    totp_enabled   BOOLEAN NOT NULL DEFAULT FALSE,
    -- The last time-step this account successfully used, so a code
    -- cannot be presented twice. Without it a six-digit code stays
    -- usable for its whole validity window - 90 seconds, given the
    -- one-step skew allowed either side of now - and anyone who
    -- observes one (shoulder-surfing, a phishing page relaying it, a
    -- logged form post) can replay it inside that window. RFC 6238
    -- section 5.2 asks implementations to make each code single-use.
    -- 0 means no code has been accepted yet.
    totp_last_step BIGINT NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at  TIMESTAMPTZ
);

-- +goose Down
DROP TABLE IF EXISTS users;
