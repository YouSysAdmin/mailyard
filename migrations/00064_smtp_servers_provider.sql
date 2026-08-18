-- How a server row is REACHED, on both server tables.
--
-- The default keeps every existing row an SMTP dial, so nothing about
-- current delivery changes.
--
-- A column and not a second table: ResolveCandidates is the one answer
-- to "what can carry this", with three callers that must agree, and a
-- parallel table would be a fourth resolution path - plus groups,
-- priority, failover, allowed_emails and the sender rules said twice.
--
-- CREDENTIALS ARE NOT HERE. A key pair goes in username and password,
-- which is already sealed by core/crypto. provider_config holds what
-- is not secret - a region, a configuration set - so the console can
-- show it without the encryption key.

-- +goose Up
ALTER TABLE smtp_servers
    ADD COLUMN provider        TEXT NOT NULL DEFAULT 'smtp',
    ADD COLUMN provider_config TEXT NOT NULL DEFAULT '{}';

ALTER TABLE shared_smtp_servers
    ADD COLUMN provider        TEXT NOT NULL DEFAULT 'smtp',
    ADD COLUMN provider_config TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE smtp_servers
    DROP COLUMN IF EXISTS provider,
    DROP COLUMN IF EXISTS provider_config;

ALTER TABLE shared_smtp_servers
    DROP COLUMN IF EXISTS provider,
    DROP COLUMN IF EXISTS provider_config;
