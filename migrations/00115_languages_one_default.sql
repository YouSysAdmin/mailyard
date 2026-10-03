-- Languages: at most one default per project, held by the schema.
--
-- The write clears the flag on the others first, and two concurrent
-- writes each cleared before either set, leaving both marked. Where
-- that already happened the oldest default keeps the flag.

-- +goose Up
UPDATE languages l
SET is_default = FALSE
FROM (
    SELECT id, row_number() OVER (PARTITION BY project_id ORDER BY created_at, id) AS n
    FROM languages
    WHERE is_default
) d
WHERE d.id = l.id AND d.n > 1;

CREATE UNIQUE INDEX languages_one_default_key ON languages (project_id) WHERE is_default;

-- +goose Down
DROP INDEX IF EXISTS languages_one_default_key;
