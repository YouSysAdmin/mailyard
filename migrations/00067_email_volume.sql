-- +goose Up
-- What a project has been ACCEPTED to send, per minute.
--
-- The plan check ran two COUNT(*) over the emails table on every send,
-- 14-30ms on 1.2M rows and growing with the project's own traffic - so
-- the busier it got, the more each send paid to ask whether it was
-- allowed.
--
-- MINUTE buckets, because the window has to stay rolling. Hour buckets
-- would turn "in the last hour" into "since the top of the hour", which
-- lets a project send its whole hourly limit at 10:59 and again at
-- 11:00.
--
-- Only ever incremented, at accept time. Nothing can decrement it, so
-- unlike a per-status counter it has nothing to drift from. It counts
-- what was ACCEPTED, which is what a plan bounds - a message that later
-- fails still used the quota.
CREATE TABLE email_volume (
    project_id UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    minute     TIMESTAMPTZ NOT NULL,
    accepted   BIGINT      NOT NULL DEFAULT 0,
    PRIMARY KEY (project_id, minute)
);

-- The read is always a range over one project, newest first.
CREATE INDEX idx_email_volume_project ON email_volume(project_id, minute DESC);

-- Backfill what the limits read, or an upgrade hands every project a
-- fresh empty hour and they can send their limit twice. Two days,
-- because the daily window is 24 hours and retention keeps two.
INSERT INTO email_volume (project_id, minute, accepted)
SELECT project_id, date_trunc('minute', created_at), COUNT(*)
FROM emails
WHERE created_at >= now() - INTERVAL '2 days'
GROUP BY project_id, date_trunc('minute', created_at)
ON CONFLICT (project_id, minute) DO UPDATE SET accepted = EXCLUDED.accepted;

-- +goose Down
DROP TABLE IF EXISTS email_volume;
