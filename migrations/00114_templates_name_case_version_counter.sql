-- Templates: names unique without regard to case, a version counter
-- that never goes back, and an active version that must exist.
--
-- A send names a template, and nothing else in the API is
-- case-sensitive, so `Welcome` and `welcome` cannot be two templates.
-- Existing pairs keep the older name and the newer ones get a suffix
-- from their own id, which is unique.
--
-- last_version is the highest number ever handed out. Numbering from
-- MAX(version) reused a number after the highest version was deleted.
--
-- The foreign key on active_version_id is what stops a version being
-- deleted while another request activates it. NO ACTION, not
-- RESTRICT: deleting a template takes its versions by cascade in the
-- same statement, and the check then runs once the row naming them
-- is gone.

-- +goose Up
UPDATE templates t
SET name = left(t.name, 87) || '-' || right(t.id::text, 12)
FROM (
    SELECT id, row_number() OVER (PARTITION BY project_id, lower(name) ORDER BY created_at, id) AS n
    FROM templates
) d
WHERE d.id = t.id AND d.n > 1;

ALTER TABLE templates DROP CONSTRAINT IF EXISTS templates_project_id_name_key;
CREATE UNIQUE INDEX templates_project_lower_name_key ON templates (project_id, lower(name));

ALTER TABLE templates ADD COLUMN last_version INTEGER NOT NULL DEFAULT 0;
UPDATE templates t
SET last_version = v.max
FROM (SELECT template_id, MAX(version) AS max FROM template_versions GROUP BY template_id) v
WHERE v.template_id = t.id;

UPDATE templates t
SET active_version_id = NULL
WHERE active_version_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM template_versions v WHERE v.id = t.active_version_id);

ALTER TABLE templates
    ADD CONSTRAINT templates_active_version_id_fkey
    FOREIGN KEY (active_version_id) REFERENCES template_versions(id);
CREATE INDEX idx_templates_active_version ON templates(active_version_id);

-- +goose Down
DROP INDEX IF EXISTS idx_templates_active_version;
ALTER TABLE templates DROP CONSTRAINT IF EXISTS templates_active_version_id_fkey;
ALTER TABLE templates DROP COLUMN IF EXISTS last_version;
DROP INDEX IF EXISTS templates_project_lower_name_key;
ALTER TABLE templates ADD CONSTRAINT templates_project_id_name_key UNIQUE (project_id, name);
