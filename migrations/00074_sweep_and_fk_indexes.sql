-- Indexes for the two kinds of query nothing was indexing: the retention
-- sweeps, and the foreign keys a parent DELETE has to check.
--
-- Both were invisible for the same reason. Neither is a request anybody
-- watches: a sweep is a scheduled job whose only output is a row count,
-- and an FK check is work Postgres does inside somebody else's DELETE. So
-- a sequential scan there costs an hour of disk a night, or turns one
-- delete into a table scan, and nothing ever reports it.
--
-- THE SWEEPS. Every one of these deletes by time and by nothing else, and
-- every index that existed led with project_id or webhook_id or category -
-- none of which the sweep names, so none of which it can use:
--
--   audit_events        created_at < ?        idx_audit_proj (category, project_id, created_at)
--   sessions            expires_at < ?        idx_sessions_user (user_id, last_seen_at)
--   webhook_deliveries  created_at < ?        idx_webhook_deliveries (webhook_id, created_at)
--   inbound_emails      received_at < ?       idx_inbound_proj_received (project_id, received_at)
--   tracking_events     created_at < ?        idx_tracking_events_msg (campaign_message_id, created_at)
--   notifications       created_at < ? AND read_at IS NOT NULL
--
-- tracking_events is the one an ATTACKER can grow. The open pixel and the
-- click redirect are unauthenticated - the HMAC in the URL is the whole
-- authority, and it never expires - so a URL out of any recipient's
-- mailbox is replayable for good, and every replay wrote a row here. The
-- handler now stops at trackedEventsPerEmail, so the growth is bounded at
-- the source as well as being sweepable.
--
-- These are the tables that grow per REQUEST, per SIGN-IN and per EVENT,
-- which is exactly why they have retention windows at all. sandbox_emails
-- already had idx_sandbox_expires and is the shape the rest now follow.
-- emails needs nothing: it is partitioned by created_at, so the planner
-- prunes, and whole weeks leave by DROP TABLE.
--
-- Measured on 200k audit_events, deleting the oldest one percent - which
-- is what a nightly sweep against a settled window actually does:
--
--   without   Seq Scan, 2001 rows,  23.9 ms
--   with      Index Scan, 2001 rows, 0.73 ms
--
-- Thirty-odd times, and it is the SHAPE that matters rather than the
-- figure: the seq scan reads the whole table, so its cost grows with
-- everything ever recorded while the index scan's cost grows with the
-- tail being removed. The generic plan uses the index too, checked with
-- EXPLAIN (GENERIC_PLAN), so a prepared statement gets it as well.
--
-- The notifications one is PARTIAL on read_at IS NOT NULL, matching the
-- sweep exactly - an unread notification is never purged however old, so
-- indexing them is paying for rows the query cannot return.
--
-- THE FOREIGN KEYS. Postgres indexes the referenced side automatically
-- and never the referencing side, so a parent DELETE scans the child
-- unless somebody says otherwise. Five had nothing usable - the composite
-- indexes that exist lead with the wrong column, and a btree cannot be
-- entered on its second column:
--
--   tracked_links.campaign_id             had (project_id, campaign_id, hash)
--   template_attachments.template_id      had (project_id, template_id)
--   template_versions.stylesheet_id       had nothing
--   subscriber_list_members.subscriber_id had (list_id, subscriber_id)
--   subscriber_list_unsubscribes.subscriber_id  likewise
--
-- tracked_links is the one that matters most and is the newest: migration
-- 00071 added that foreign key, so deleting a campaign began scanning a
-- table that grows per campaign per link. The subscriber pair is the one
-- an operator meets, because removing a subscriber is an ordinary act and
-- it scanned both membership tables.
--
-- Measured, and the first measurement was WRONG in a way worth recording.
-- With 50k tracked_links all belonging to ONE campaign, deleting that
-- campaign took 15.8 ms with the index and 15.0 ms without: no
-- difference, because the cascade has to delete all 50k rows either way
-- and finding them is not the work. The index earns its keep on the shape
-- the table actually has - 50k rows across a HUNDRED campaigns, deleting
-- one of them:
--
--   with     0.49 ms
--   without  3.03 ms
--
-- Six times here, and again the shape is the point: without the index the
-- scan grows with every campaign the project has ever run, with it the
-- cost stays with the 500 rows being removed.
--
-- Written as one migration because they are one omission with one cause,
-- and reading them together is what makes the cause legible.
--
-- Plain CREATE INDEX, not CONCURRENTLY. On a fresh install these tables
-- are empty and it is instantaneous. On an existing one it takes a SHARE
-- lock, so reads continue and writes to that one table wait - which is
-- how every other migration here behaves, and the serving nodes are
-- already refusing an older schema while this runs.

-- +goose Up

-- Retention sweeps.
CREATE INDEX idx_audit_events_created ON audit_events(created_at);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);
CREATE INDEX idx_webhook_deliveries_created ON webhook_deliveries(created_at);
CREATE INDEX idx_inbound_received ON inbound_emails(received_at);
CREATE INDEX idx_tracking_events_created ON tracking_events(created_at);
CREATE INDEX idx_notifications_purge ON notifications(created_at)
    WHERE read_at IS NOT NULL;

-- Foreign keys, so a parent DELETE seeks instead of scanning.
CREATE INDEX idx_tracked_links_campaign ON tracked_links(campaign_id);
CREATE INDEX idx_template_attachments_template ON template_attachments(template_id);
CREATE INDEX idx_template_versions_stylesheet ON template_versions(stylesheet_id);
CREATE INDEX idx_slm_subscriber ON subscriber_list_members(subscriber_id);
CREATE INDEX idx_slu_subscriber ON subscriber_list_unsubscribes(subscriber_id);

-- +goose Down
DROP INDEX IF EXISTS idx_audit_events_created;
DROP INDEX IF EXISTS idx_sessions_expires;
DROP INDEX IF EXISTS idx_webhook_deliveries_created;
DROP INDEX IF EXISTS idx_inbound_received;
DROP INDEX IF EXISTS idx_tracking_events_created;
DROP INDEX IF EXISTS idx_notifications_purge;
DROP INDEX IF EXISTS idx_tracked_links_campaign;
DROP INDEX IF EXISTS idx_template_attachments_template;
DROP INDEX IF EXISTS idx_template_versions_stylesheet;
DROP INDEX IF EXISTS idx_slm_subscriber;
DROP INDEX IF EXISTS idx_slu_subscriber;
