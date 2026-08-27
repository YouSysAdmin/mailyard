-- A relay node that PULLS its mail rather than listening for it.
--
-- relay_nodes.mode says which: 'listen' is a node the delivery worker
-- dials over mutual TLS, 'pull' is a node behind NAT or an egress proxy
-- that cannot be dialled and fetches assigned mail over the control
-- channel instead.
--
-- relay_assignments is one row per message handed to a pull node. The
-- email row itself stays 'processing' for as long as the assignment is
-- live, so every in-flight rule (partition drops, erasure, the stuck
-- sweep) keeps working unchanged - the sweep only has to look here
-- before it takes a processing row back. The raw bytes live in this
-- table and not on the partitioned emails row: they are large, they
-- are read by exactly one node, and they are gone on finalize.

-- +goose Up
ALTER TABLE relay_nodes
    ADD COLUMN mode TEXT NOT NULL DEFAULT 'listen';

CREATE TABLE relay_assignments (
    email_id         UUID PRIMARY KEY,
    node_id          UUID NOT NULL REFERENCES relay_nodes(id) ON DELETE CASCADE,

    -- The delivery row the node is, for delivered_via on finalize.
    server_id        UUID NOT NULL,

    -- The email row's partition key, so finalize and requeue name it
    -- instead of visiting every partition.
    email_created_at TIMESTAMPTZ NOT NULL,

    envelope_from    TEXT NOT NULL DEFAULT '',
    -- Recipients still to be delivered, JSON array. Shrinks as the node
    -- reports them.
    recipients       TEXT NOT NULL DEFAULT '[]',
    raw              BYTEA NOT NULL,
    -- How many recipients the node has reported delivered so far, so a
    -- message finished over several reports is 'sent' when any were.
    delivered        INTEGER NOT NULL DEFAULT 0,

    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Past this the platform takes the message back and offers it to
    -- the next candidate. A node extends it by saying it still holds
    -- the message.
    expires_at       TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_relay_assignments_node ON relay_assignments (node_id, created_at);
CREATE INDEX idx_relay_assignments_expiry ON relay_assignments (expires_at);

-- +goose Down
DROP TABLE IF EXISTS relay_assignments;
ALTER TABLE relay_nodes DROP COLUMN IF EXISTS mode;
