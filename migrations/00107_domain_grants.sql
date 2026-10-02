-- A verified domain shared by its owner with another project.
--
-- The owner stays the one row in domains: it publishes the DNS
-- records, holds the DKIM key and receives the inbound mail, because
-- an MX recipient and a DKIM selector each need exactly one answer.
-- A grant lets project_id SEND as the domain and its subdomains,
-- signed with the owner's key, through its own servers. Dropping the
-- domain or either project drops the grant.

-- +goose Up
CREATE TABLE domain_grants (
    domain_id  UUID NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    granted_by TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (domain_id, project_id)
);

CREATE INDEX idx_domain_grants_project ON domain_grants (project_id);

-- +goose Down
DROP TABLE IF EXISTS domain_grants;
