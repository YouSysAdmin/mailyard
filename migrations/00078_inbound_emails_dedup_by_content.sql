-- The Message-ID alone no longer decides a duplicate.
--
-- 00073 made (project_id, message_id) UNIQUE, and a Message-ID is whatever
-- the sender typed. Anyone who could predict the next one - a sequential
-- generator, the References of a thread they were on - sent junk under it
-- first, and the real message was then "already ingested": answered 250
-- and dropped, while the junk was what the webhook delivered. Mail lost
-- with a receipt saying it arrived.
--
-- So the id joins the content signature in dedup_hash instead (see
-- inbound.dedupHash), where a retry of the same message is still one row
-- and a different message under a reused id is a different message.
-- idx_inbound_dedup stays UNIQUE and partial exactly as 00073 left it. The
-- Message-ID index goes back to a plain one: nothing looks a message up
-- by it any more, but the column is still displayed and filtered on.

-- +goose Up
DROP INDEX IF EXISTS idx_inbound_message_id;
CREATE INDEX idx_inbound_message_id ON inbound_emails(project_id, message_id);

-- +goose Down
DROP INDEX IF EXISTS idx_inbound_message_id;
CREATE UNIQUE INDEX idx_inbound_message_id ON inbound_emails(project_id, message_id)
    WHERE message_id <> '';
