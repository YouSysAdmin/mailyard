-- Which sender DOMAINS may leave through one of a project's own
-- servers.
--
-- shared_smtp_servers has carried this since 00020 and smtp_servers has
-- not, so a project could only restrict a server by allowed_emails -
-- per ADDRESS, and empty by default. A project sending as several
-- domains through several relay nodes therefore had no way to say which
-- node carries which domain, and every node carried everything. The
-- symptom is not an error: the message leaves from a machine that is
-- not in that domain's SPF record, and the sender learns of it from a
-- deliverability report weeks later.
--
-- JSON array of bare domains, matched EXACTLY - see Server.AllowsDomain
-- for why a listed name deliberately does not cover its subdomains.
--
-- Empty allows any, which is what every existing row gets.

-- +goose Up
ALTER TABLE smtp_servers ADD COLUMN allowed_domains TEXT NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE smtp_servers DROP COLUMN IF EXISTS allowed_domains;
