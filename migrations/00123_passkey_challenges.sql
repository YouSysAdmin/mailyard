-- Spent WebAuthn challenges.
--
-- A ceremony rides in a sealed cookie, and a cookie copied before it
-- was cleared would answer its challenge again on any node. A finished
-- ceremony records its challenge here, and a second finish with the
-- same one is refused. Rows only matter until the ceremony would have
-- expired anyway, so each claim prunes the expired ones.

-- +goose Up
CREATE TABLE passkey_challenges (
    challenge  TEXT PRIMARY KEY,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_passkey_challenges_expires ON passkey_challenges (expires_at);

-- +goose Down
DROP TABLE IF EXISTS passkey_challenges;
