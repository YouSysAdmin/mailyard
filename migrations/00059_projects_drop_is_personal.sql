-- +goose Up
-- The personal project is gone as a concept, so the flag that marked
-- one has nothing left to say.
--
-- It carried a rule: such a project could not be deleted, because
-- every account was assumed to need one. Belonging to no project is an
-- ordinary state now, and a platform route deleted these in bulk
-- anyway - which locked out anyone whose personal project was swept
-- up, since the console kept sending the dead id and every route
-- refused it, including the one that lists projects.
--
-- Existing rows keep their contents and stop being special.
ALTER TABLE projects DROP COLUMN is_personal;

-- The setting that governed minting them goes with the concept. An
-- unknown key is refused on write, so a row for one nothing reads
-- would be unreachable rather than merely stale.
DELETE FROM settings WHERE key = 'personal_projects_enabled';

-- +goose Down
ALTER TABLE projects ADD COLUMN is_personal BOOLEAN NOT NULL DEFAULT FALSE;
