-- +goose Up
-- The delivery trend, per project per day per status.
--
-- The chart asked the emails table directly: two GROUP BY queries over
-- fourteen days, 434-698ms each on 1.2M rows, paid again on every
-- refresh. An index does not help - grouping a million rows means
-- reading a million index entries.
--
-- RECOMPUTED, never incremented, which is the opposite of email_volume
-- next door. Volume counts what was accepted and only goes up, so it
-- cannot drift. This counts OUTCOMES - queued becomes sent, a retry
-- turns failed back into sent - and a counter would need a decrement at
-- every one of those transitions. A recomputation runs the same GROUP
-- BY the chart used to, so it cannot come to mean something else.
CREATE TABLE email_daily (
    project_id UUID   NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    day        DATE   NOT NULL,
    status     TEXT   NOT NULL,
    n          BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (project_id, day, status)
);

-- Every read is one project over a range of days.
CREATE INDEX idx_email_daily_project ON email_daily(project_id, day DESC);

-- Backfill everything, so the chart is right the moment the upgrade
-- finishes instead of growing back over two weeks. Bucketed to a UTC
-- date like the recompute does - a local day would redraw history.
INSERT INTO email_daily (project_id, day, status, n)
SELECT project_id, (created_at AT TIME ZONE 'UTC')::date, status, COUNT(*)
FROM emails
GROUP BY project_id, (created_at AT TIME ZONE 'UTC')::date, status
ON CONFLICT (project_id, day, status) DO UPDATE SET n = EXCLUDED.n;

-- +goose Down
DROP TABLE IF EXISTS email_daily;
