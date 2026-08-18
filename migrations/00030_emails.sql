-- The outbound delivery log, RANGE partitioned by week.
--
-- The only table that grows per MESSAGE. Retention on it was one
-- unbounded DELETE over millions of rows - a long transaction, a WAL
-- burst, replication lag and bloat autovacuum then chased for weeks.
-- A whole week now goes in one DROP TABLE, and the indexes are per
-- week too.
--
-- Weekly and not monthly because retention defaults to thirty days: a
-- monthly partition would only be droppable whole for a few days a
-- month. About five are live at a time.
--
-- What it costs:
--
--   * The primary key must include the partition key, so it is
--     (id, created_at). Uniqueness of id alone is ids.New()'s job now,
--     not the database's.
--   * A lookup by id alone visits every live partition. Requeue and
--     Finalize take created_at, which prunes them to one. Bounce
--     intake, the tracking pixel and the console detail page do fan
--     out - those are per-bounce and per-open, not per-send.
--   * Something has to create next week's partition. That is
--     internal/core/partition, and emails_default below is the
--     backstop for the day it does not run.

-- +goose Up

CREATE TABLE emails (
    id                     UUID NOT NULL,
    project_id             UUID NOT NULL REFERENCES projects(id),
    created_by              TEXT NOT NULL DEFAULT '',
    api_key_id             UUID,
    -- Optional pinned server, an INPUT to routing. A pinned server
    -- never falls back.
    smtp_server_id         UUID,
    sender                  TEXT NOT NULL,
    -- JSON array of recipient addresses.
    recipients              TEXT NOT NULL,
    subject                 TEXT NOT NULL,
    template_name           TEXT NOT NULL DEFAULT '',
    html_body               TEXT NOT NULL DEFAULT '',
    text_body               TEXT NOT NULL DEFAULT '',
    attachments_json        TEXT NOT NULL DEFAULT '[]',
    headers_json            TEXT NOT NULL DEFAULT '{}',
    -- RFC 2369 / 8058 headers, plus the opt-out list the send is scoped
    -- to. Both forms are stored because a caller-supplied header
    -- carries both, and keeping one would drop the other.
    list_unsubscribe_url    TEXT NOT NULL DEFAULT '',
    list_unsubscribe_mailto TEXT NOT NULL DEFAULT '',
    list_unsubscribe_post   BOOLEAN NOT NULL DEFAULT FALSE,
    unsubscribe_list_id    UUID,
    status                  TEXT NOT NULL DEFAULT 'pending',
    error_message           TEXT NOT NULL DEFAULT '',
    attempts                INTEGER NOT NULL DEFAULT 0,
    max_attempts            INTEGER NOT NULL DEFAULT 5,
    -- Queue state: due time for queued and scheduled rows, claim
    -- stamp for crash recovery of processing rows.
    next_attempt_at         TIMESTAMPTZ,
    claimed_at              TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    scheduled_at            TIMESTAMPTZ,
    sent_at                 TIMESTAMPTZ,
    -- Which pool this message is routed to. Empty means the project's
    -- default group. An id rather than a slug: the slug is the
    -- caller-facing handle and is resolved once, at accept time.
    smtp_group_id          UUID,
    -- Per-message tracking state. A transactional send has no
    -- campaign_message row to hold it, which is why tracking outside a
    -- campaign needed these. `tracked` tells "nobody opened it" from
    -- "we never asked".
    opened_at               TIMESTAMPTZ,
    clicked_at              TIMESTAMPTZ,
    open_count              INTEGER NOT NULL DEFAULT 0,
    click_count             INTEGER NOT NULL DEFAULT 0,
    tracked                 BOOLEAN NOT NULL DEFAULT FALSE,
    -- Which server actually CARRIED the message, filled on success.
    -- Not smtp_server_id, which is the server the sender PINNED:
    -- writing the winner there would silently repin a failed-over
    -- message to B on its next retry.
    delivered_via           TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- The log listing, and the only index that prunes by itself.
CREATE INDEX idx_emails_proj_created ON emails (project_id, created_at DESC);

-- The worker's claim query, PARTIAL.
--
-- The claim has no date predicate, so it reads every partition. That
-- is affordable only because the index holds the in-flight statuses
-- only: an old partition's copy is empty. A full index would make the
-- claim slower every week. processing is in the set because
-- RecoverStuck looks for exactly that.
CREATE INDEX idx_emails_queue ON emails (status, next_attempt_at)
    WHERE status IN ('queued', 'scheduled', 'processing');

-- This week plus four, so a fresh install works before the scheduled
-- job has run and a job that fails for a fortnight still has room.
--
-- date_trunc('week') is Monday, and internal/core/partition computes
-- the same boundary in Go - a test pins them together, because a day's
-- disagreement makes every later CREATE overlap and fail.
--
-- Named by start date, not ISO week number: weeks 1 and 53 straddle
-- new year.
-- +goose StatementBegin
DO $$
DECLARE
    cur  DATE;
    hi   DATE;
    part TEXT;
BEGIN
    cur := date_trunc('week', now())::date;
    hi  := (date_trunc('week', now()) + INTERVAL '4 weeks')::date;

    WHILE cur <= hi LOOP
        part := 'emails_w' || to_char(cur, 'YYYY_MM_DD');
        EXECUTE format(
            'CREATE TABLE IF NOT EXISTS %I PARTITION OF emails FOR VALUES FROM (%L) TO (%L)',
            part, cur, (cur + INTERVAL '1 week')::date);
        cur := (cur + INTERVAL '1 week')::date;
    END LOOP;
END $$;
-- +goose StatementEnd

-- The backstop, so an insert never fails because a job did not run.
-- Rows landing here are reported as an error: they block creating the
-- real partition for that week.
CREATE TABLE emails_default PARTITION OF emails DEFAULT;

-- +goose Down
DROP TABLE IF EXISTS emails;
