-- A relay node that also RECEIVES: what it reports about that half.
--
-- Three columns rather than none, because an MX that is quietly not
-- forwarding is invisible otherwise. Mail keeps arriving at the node,
-- the console keeps showing a healthy node, and the only symptom is
-- bounces that stop appearing - which looks exactly like nothing
-- bouncing.

-- +goose Up
ALTER TABLE relay_nodes
    -- Whether this node runs an MX at all. Reported by the node on
    -- every heartbeat rather than configured here: it follows from
    -- that machine's own config file, and a second copy on this side
    -- would be one that disagrees the moment somebody edits the first.
    ADD COLUMN inbound_enabled BOOLEAN NOT NULL DEFAULT FALSE,

    -- How much mail the node is holding that it has not managed to
    -- forward. A number that only grows is the whole diagnosis: the
    -- node is fine, the link is not.
    ADD COLUMN inbound_queued INTEGER NOT NULL DEFAULT 0,

    -- When this node last successfully handed us a message. Stamped
    -- HERE, when the forward is accepted, not reported by the node -
    -- the same rule public_ip follows, for the same reason.
    ADD COLUMN last_inbound_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE relay_nodes
    DROP COLUMN IF EXISTS inbound_enabled,
    DROP COLUMN IF EXISTS inbound_queued,
    DROP COLUMN IF EXISTS last_inbound_at;
