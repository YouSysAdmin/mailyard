-- A domain CLAIM is per project until it verifies.
--
-- The name used to be unique across the installation from the moment
-- it was typed, so a project that claimed a name it could never prove
-- held it from its real owner for good. Now each project may hold one
-- claim per name, at most one row per name is VERIFIED - the MX
-- listener and every ownership check still find exactly one owner -
-- and the first project to verify drops the others' stale claims.
--
-- Names are stored without a trailing dot. A dotted claim was a second
-- spelling of the same name that no lookup ever matched, so a dotted
-- row whose bare name is already held in the same project, or verified
-- beside it, is removed and the rest are respelled.

-- +goose Up
ALTER TABLE domains DROP CONSTRAINT IF EXISTS domains_domain_key;
ALTER TABLE domains ADD CONSTRAINT domains_project_domain_key UNIQUE (project_id, domain);
CREATE UNIQUE INDEX domains_verified_domain_key ON domains (domain) WHERE verified;

DELETE FROM domains d
WHERE d.domain LIKE '%.'
  AND EXISTS (
      SELECT 1 FROM domains o
      WHERE o.id <> d.id AND o.domain = rtrim(d.domain, '.')
        AND (o.project_id = d.project_id OR (o.verified AND d.verified))
  );

UPDATE domains SET domain = rtrim(domain, '.') WHERE domain LIKE '%.';

-- +goose Down
DROP INDEX IF EXISTS domains_verified_domain_key;
ALTER TABLE domains DROP CONSTRAINT IF EXISTS domains_project_domain_key;
ALTER TABLE domains ADD CONSTRAINT domains_domain_key UNIQUE (domain);
