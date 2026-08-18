-- Passkeys (WebAuthn) on console accounts.
--
-- A row per authenticator rather than a blob on the user, because
-- sign-in resolves the owner from the credential id alone - a
-- discoverable login carries no email, and a blob would mean scanning
-- every user row on an unauthenticated endpoint.
--
-- No user handle column: the account uuid IS the WebAuthn handle. It
-- is already opaque, carries no personal information as the spec
-- requires, and is one identifier instead of two that can drift.

-- +goose Up
CREATE TABLE user_passkeys (
    id                     UUID PRIMARY KEY,
    user_id                UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- The authenticator's raw credential id, base64url encoded.
    -- Unique across the install: an assertion names this and nothing
    -- else, so two accounts holding it would make the owner
    -- ambiguous.
    credential_id TEXT NOT NULL UNIQUE,
    -- What the account holder called it, so three passkeys can be
    -- told apart when one needs revoking.
    name          TEXT NOT NULL DEFAULT '',
    -- The go-webauthn credential as JSON: public key, transports, and
    -- the sign counter. No secret - the private half never leaves the
    -- authenticator, which is why this column is NOT sealed while
    -- every other credential column here is.
    credential    TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at  TIMESTAMPTZ
);

CREATE INDEX idx_user_passkeys_user ON user_passkeys (user_id);

-- +goose Down
DROP TABLE IF EXISTS user_passkeys;
