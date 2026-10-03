-- An unsubscribe list name is unique within its project without regard
-- to case, like every name a person types into the API.
--
-- Lists that already differ only in case keep the oldest name as it is,
-- and every later one gets its id prefix appended, so the index can be
-- built without deleting anything. Ids are untouched, so the signed
-- links already delivered keep working.

-- +goose Up
UPDATE unsubscribe_lists u
SET name = u.name || '-' || left(u.id::text, 8)
WHERE EXISTS (
    SELECT 1 FROM unsubscribe_lists o
    WHERE o.project_id = u.project_id
      AND lower(o.name) = lower(u.name)
      AND (o.created_at, o.id) < (u.created_at, u.id)
);

CREATE UNIQUE INDEX idx_unsubscribe_lists_project_lower_name
    ON unsubscribe_lists (project_id, lower(name));

-- +goose Down
DROP INDEX IF EXISTS idx_unsubscribe_lists_project_lower_name;
