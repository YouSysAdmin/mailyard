-- Drop the EMPTY FUTURE weekly partitions of `emails`, again, correctly.
--
-- 00075 meant to do this and did nothing. Its emptiness test was
--
--     EXECUTE format('SELECT 1 FROM %I LIMIT 1', r.name);
--     CONTINUE WHEN FOUND;
--
-- and PL/pgSQL does not set FOUND from an EXECUTE without INTO - measured
-- on a live server: FOUND read true after that statement against an empty
-- table and a populated one alike. So every partition took the CONTINUE
-- and none was dropped. The safe way to be wrong, which is why nothing
-- noticed: no data went anywhere, the weekly partitions simply aged out
-- on their own schedule and the daily switch waited on them.
--
-- The three conditions and their reasons are 00075's, unchanged. The
-- test is EXISTS read INTO a variable, which is what FOUND was believed
-- to be. Idempotent: an installation where the weeklies have since aged
-- out finds nothing to drop.

-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE
    r record;
    has_rows boolean;
BEGIN
    FOR r IN
        SELECT c.relname AS name,
               pg_get_expr(c.relpartbound, c.oid) AS bound
        FROM pg_class c
        JOIN pg_inherits i ON i.inhrelid = c.oid
        WHERE i.inhparent = to_regclass('emails')::oid
          AND c.relname <> 'emails_default'
          AND c.relpartbound IS NOT NULL
    LOOP
        CONTINUE WHEN substring(r.bound from 'FROM \(''([^'']+)''\)') IS NULL;
        CONTINUE WHEN substring(r.bound from 'FROM \(''([^'']+)''\)')::timestamptz <= now();

        EXECUTE format('SELECT EXISTS (SELECT 1 FROM %I)', r.name) INTO has_rows;
        CONTINUE WHEN has_rows;

        EXECUTE format('DROP TABLE %I', r.name);
        RAISE NOTICE 'dropped empty future partition %, the daily maintainer will cover its range', r.name;
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- Nothing to undo, as in 00075: the partitions dropped were empty and in
-- the future, and internal/core/partition recreates coverage on its next
-- run.
