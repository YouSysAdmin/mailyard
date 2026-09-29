-- sender is the From header of a received message, who it says it is
-- from. The SMTP MAIL FROM moves to bounce_address: it is where bounces
-- go and what SPF judges, and not the sender.
-- A row stored without parsed headers has no From and an empty sender.

-- +goose Up
ALTER TABLE inbound_emails ADD COLUMN bounce_address TEXT NOT NULL DEFAULT '';

UPDATE inbound_emails
SET bounce_address = sender,
    sender = COALESCE(NULLIF(headers, '')::jsonb ->> 'From', '');

-- +goose Down
UPDATE inbound_emails SET sender = bounce_address;
ALTER TABLE inbound_emails DROP COLUMN IF EXISTS bounce_address;
