-- API keys carry permissions from the catalogue, not scopes.
--
-- The seven scope strings were a second vocabulary nothing translated
-- into the first, and one of them (read) spanned nine resources. The
-- middleware resolved every key to the owner preset anyway, so the
-- scope list was the only thing narrowing an otherwise
-- owner-equivalent credential.
--
-- Same encoding as the project role permissions, because it is the
-- same catalogue read by the same middleware.
--
-- DEFAULT '[]' means nothing, not send: a key mints with permissions
-- or it mints useless.

-- +goose Up
ALTER TABLE api_keys ADD COLUMN permissions TEXT NOT NULL DEFAULT '[]';
ALTER TABLE api_keys DROP COLUMN scopes;

-- +goose Down
ALTER TABLE api_keys ADD COLUMN scopes TEXT NOT NULL DEFAULT '["send"]';
ALTER TABLE api_keys DROP COLUMN permissions;
