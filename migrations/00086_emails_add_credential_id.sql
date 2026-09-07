-- Which SMTP submission credential carried the message in.
--
-- created_by already records a person, but on a submission that person
-- is whoever MINTED the credential - so the row could say who owns the
-- login and never which login was used. NULL on every other path.

-- +goose Up
ALTER TABLE emails
    ADD COLUMN credential_id UUID;

-- +goose Down
ALTER TABLE emails
    DROP COLUMN IF EXISTS credential_id;
