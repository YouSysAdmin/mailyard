-- Inbound deduplication is a RULE, not a lookup.
--
-- Ingest read the table for the Message-ID (or, for a message carrying
-- none, a content hash) and inserted when it found nothing. Two
-- deliveries of the same message in the same instant both found nothing
-- and both inserted: idx_inbound_message_id and idx_inbound_dedup were
-- plain indexes, so nothing refused the second row. The INSERT even
-- carried ON CONFLICT(id) DO UPDATE, which reads as protection and is
-- not - the id is freshly minted per attempt, so that clause can never
-- fire.
--
-- An MTA retry is the ordinary case, not an exotic one. A sender whose
-- first attempt times out after our 250 retries within seconds, and a
-- forwarder can fan the same message at several of our MX addresses at
-- once. Each surviving duplicate is a second inbound_received webhook,
-- and for a bounce report a second bounce row and a second suppression -
-- so a retried report could suppress an address the first report had
-- already recorded, doubling the history an operator reads.
--
-- PARTIAL, on <> '', because both columns default to '' and a row can
-- legitimately have neither: a message rejected at the suppression check
-- or by DMARC is persisted for the audit trail before parsing, so it has
-- no Message-ID, and so does a message whose MIME failed to parse. A
-- full unique index would let exactly one such row exist per project and
-- refuse every rejection after it. The read side never looks either
-- column up by '' (both finders return early on an empty argument), so
-- the partial index serves every query the plain one did and these are
-- dropped rather than kept beside it.
--
-- Existing duplicates are deleted first, keeping the row that arrived
-- first, because CREATE UNIQUE INDEX validates what is already there.
-- The blobs those rows named are not reachable from here - see the note
-- in migration 00070 - but a duplicate's attachments are the same
-- attachments, and the row that survives names its own copies.

-- +goose Up
DELETE FROM inbound_emails a
WHERE a.message_id <> ''
  AND EXISTS (
      SELECT 1 FROM inbound_emails b
      WHERE b.project_id = a.project_id
        AND b.message_id = a.message_id
        AND (b.received_at, b.id) < (a.received_at, a.id)
  );

DELETE FROM inbound_emails a
WHERE a.dedup_hash <> ''
  AND EXISTS (
      SELECT 1 FROM inbound_emails b
      WHERE b.project_id = a.project_id
        AND b.dedup_hash = a.dedup_hash
        AND (b.received_at, b.id) < (a.received_at, a.id)
  );

DROP INDEX IF EXISTS idx_inbound_message_id;
DROP INDEX IF EXISTS idx_inbound_dedup;

CREATE UNIQUE INDEX idx_inbound_message_id ON inbound_emails(project_id, message_id)
    WHERE message_id <> '';
CREATE UNIQUE INDEX idx_inbound_dedup ON inbound_emails(project_id, dedup_hash)
    WHERE dedup_hash <> '';

-- +goose Down
DROP INDEX IF EXISTS idx_inbound_message_id;
DROP INDEX IF EXISTS idx_inbound_dedup;

CREATE INDEX idx_inbound_message_id ON inbound_emails(project_id, message_id);
CREATE INDEX idx_inbound_dedup ON inbound_emails(project_id, dedup_hash);
