-- The envelope of a capture holds bare addresses, the way the SMTP
-- surface records it. Captures taken over the API stored the mailbox
-- form ("Name <addr>"), which no address filter matches, so those are
-- reduced to the address inside the angle brackets.

-- +goose Up
UPDATE sandbox_emails
SET sender = substring(sender FROM '<([^<>]+)>\s*$')
WHERE sender ~ '<[^<>]+>\s*$';

UPDATE sandbox_emails e
SET recipients = (
    SELECT COALESCE(jsonb_agg(COALESCE(substring(r FROM '<([^<>]+)>\s*$'), r) ORDER BY n), '[]'::jsonb)::text
    FROM jsonb_array_elements_text(e.recipients::jsonb) WITH ORDINALITY AS t(r, n)
)
WHERE EXISTS (SELECT 1 FROM jsonb_array_elements_text(e.recipients::jsonb) AS x(r) WHERE x.r ~ '<[^<>]+>\s*$');

-- +goose Down
SELECT 1;
