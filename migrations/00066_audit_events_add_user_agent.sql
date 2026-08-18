-- +goose Up
-- The audit trail recorded the address a request reached us from and
-- nothing else about where it came from - and that address is not the
-- person.
--
-- An iCloud Private Relay user arrives on a shared Cloudflare, Akamai
-- or Fastly egress, and no header carries their real address: the
-- egress proxy is never told it. WARP and corporate egresses behave the
-- same. So one address is not one person, and two are not two.
--
-- The user agent is not identity either, but it is the one other thing
-- the request gives for free, and it separates a phone from a laptop
-- when the address cannot.
ALTER TABLE audit_events ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE audit_events DROP COLUMN IF EXISTS user_agent;
