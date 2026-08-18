-- Partitions of `emails` become DAILY instead of weekly.
--
-- The reason is retention, not speed. A partition can only be dropped
-- once its WHOLE range is past the cutoff, so a weekly one held up to six
-- extra days of history and then removed a week of it in one go. A day is
-- dropped a day at a time, and the window an operator configured is the
-- window they actually get.
--
-- NO DATA MOVES, and that is the point of doing it this way. Partition
-- names are already dates, Postgres allows range partitions of mixed
-- width so long as they do not overlap, and the maintainer reads each
-- bound out of the catalog rather than assuming one. So every weekly
-- partition that holds rows stays exactly where it is and ages out on its
-- own schedule, while everything from here forward is daily.
--
-- What this file DOES do is clear the way. Migration 00030 lays down the
-- current week plus four, and internal/core/partition kept that horizon
-- topped up - so without this the next four to five weeks are already
-- claimed by weekly partitions and the switch would not take effect until
-- they aged out. Dropping the EMPTY ones whose range has not started yet
-- lets the maintainer fill the same span with days on its next run.
--
-- Three conditions, all of them load-bearing:
--
--   lower bound in the future  a partition whose range has begun may hold
--                              rows written a moment ago
--   zero rows                  belt and braces. created_at is stamped at
--                              insert, so a future-bounded partition
--                              should be empty - should is not is
--   not the default            emails_default is not a range partition and
--                              is the backstop this whole scheme relies on
--
-- The current week's partition is deliberately left alone: it holds
-- today's mail. So an installation runs weekly-until-Sunday and daily
-- after, which is exactly the mixed state the maintainer is written for.
--
-- THE CEILING that makes daily safe lives in code, not here. Retention
-- defaults to 30 days, which settles at about 45 partitions - but 0 means
-- keep forever, and daily then adds 365 a year with nothing removing any.
-- Measured at 730 partitions against 105 weekly, same 2M rows, on the
-- queue claim (which carries no date predicate and so can never prune):
-- planning 94-420ms against 2ms, and 2194 relation locks per claim
-- against 319. The lock table is shared and sized from
-- max_locks_per_transaction times max_connections - 6400 by default - so
-- SIXTEEN concurrent claims failed with "out of shared memory". The same
-- sixteen against the weekly table failed none. partition.maxPartitions
-- is the alarm for that.

-- +goose Up
-- +goose StatementBegin
DO $$
DECLARE r record;
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
        -- The lower bound, read out of the rendered bound expression.
        -- There is no catalog column holding it.
        CONTINUE WHEN substring(r.bound from 'FROM \(''([^'']+)''\)') IS NULL;
        CONTINUE WHEN substring(r.bound from 'FROM \(''([^'']+)''\)')::timestamptz <= now();

        EXECUTE format('SELECT 1 FROM %I LIMIT 1', r.name);
        CONTINUE WHEN FOUND;

        EXECUTE format('DROP TABLE %I', r.name);
        RAISE NOTICE 'dropped empty future partition %, the daily maintainer will cover its range', r.name;
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- Nothing to undo. The partitions dropped above were empty and in the
-- future, and internal/core/partition recreates coverage on its next run
-- at whatever width that version uses.
SELECT 1;
