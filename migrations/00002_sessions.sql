-- Tracked sign-ins.
--
-- The row holds no secret and cannot authenticate: the session JWT is
-- still the credential, and its jti claim is this id. The row exists
-- so a live token can be revoked before it expires, and so a user can
-- see where they are signed in.

-- +goose Up
CREATE TABLE sessions (
    id                     UUID PRIMARY KEY,
    user_id                UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_agent       TEXT NOT NULL DEFAULT '',
    ip               TEXT NOT NULL DEFAULT '',
    -- Which identity provider authenticated this session, empty for a
    -- local password sign-in. Recorded here rather than derived at
    -- check time because it is a fact about how this sign-in happened:
    -- it cannot change for the life of the session, and a project
    -- that later starts requiring SSO must not retroactively bless a
    -- password session.
    auth_provider_id       UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at       TIMESTAMPTZ NOT NULL,
    revoked          BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX idx_sessions_user ON sessions(user_id, last_seen_at);

-- +goose Down
DROP TABLE IF EXISTS sessions;
