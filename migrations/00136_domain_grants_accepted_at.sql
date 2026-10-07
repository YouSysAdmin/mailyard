-- A share is an offer until the other project accepts it. Before this,
-- a grant took effect the moment the owner named a slug, so any project
-- could be made to send as a stranger's domain without a word to it,
-- and its members found the domain listed under "shared" with no way
-- to refuse. accepted_at NULL is pending: it does not cover sending and
-- the owner's grant list shows the slug it typed, not the project's
-- name, until the other side says yes.
--
-- Grants that existed before this column were working, so they are
-- carried over as accepted rather than silently stopping that mail.

-- +goose Up
ALTER TABLE domain_grants ADD COLUMN accepted_at TIMESTAMPTZ;
UPDATE domain_grants SET accepted_at = created_at;

-- +goose Down
ALTER TABLE domain_grants DROP COLUMN IF EXISTS accepted_at;
