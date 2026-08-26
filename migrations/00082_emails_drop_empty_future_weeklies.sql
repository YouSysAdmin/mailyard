-- Drop the EMPTY FUTURE weekly partitions of `emails`.
--
-- The loop and its three conditions are 00075's. The emptiness test is
-- EXISTS read INTO a variable: PL/pgSQL does not set FOUND from an
-- EXECUTE without INTO, so `CONTINUE WHEN FOUND` after a bare EXECUTE
-- continues on every row and drops nothing. Idempotent - an installation
-- whose weeklies have aged out finds nothing to drop.

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
-- Nothing to undo: the partitions dropped were empty and in the future,
-- and internal/core/partition recreates coverage on its next run.
