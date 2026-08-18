-- Egress relay nodes: identity and liveness.
--
-- A node still IS a server row - there must be ONE answer to "what
-- can carry this message" - but identity and delivery eligibility are
-- not the same question, and a node can attach to either delivery
-- table: a platform node joins shared_smtp_servers, a project's own
-- node joins smtp_servers. Held as columns on those, the fields and -
-- far worse - the freshness rule would be duplicated across both, and
-- two definitions of "this node is alive" is exactly the pair that
-- eventually disagrees.

-- +goose Up
CREATE TABLE relay_nodes (
    id                     UUID PRIMARY KEY,

    -- Empty means a platform node: it joins the shared pool and its
    -- delivery row lives in shared_smtp_servers. Otherwise it is the
    -- project that enrolled it, and the delivery row is in
    -- smtp_servers.
    --
    -- Not a foreign key, because the empty string is not a project and
    -- a nullable column would make every read ask the same question
    -- twice. Deletion is handled by the node lifecycle.
    project_id             UUID,

    -- The delivery row this node IS. Which table it points into is
    -- already decided by project_id - a second column saying so could
    -- disagree with it, and then two reads of the same node would
    -- resolve to different servers.
    server_id              UUID NOT NULL,

    -- hex(sha256) of the long-lived control token, never the token.
    -- Kept HERE and not on the server row, so the delivery path -
    -- which reads that row on every send - never loads a credential
    -- it has no use for.
    token_hash TEXT NOT NULL,

    name       TEXT NOT NULL DEFAULT '',
    version    TEXT NOT NULL DEFAULT '',

    -- The address the control plane SAW the node connect from, not
    -- one the node claimed. It has to be authorized in SPF for
    -- whichever bounce domain this node's mail leaves under, so a
    -- node naming its own address could put anything there.
    public_ip  TEXT NOT NULL DEFAULT '',

    last_seen_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL,

    -- One node per delivery row, in either direction.
    UNIQUE (server_id)
);

-- The join every pick query makes, and the project listing.
CREATE INDEX idx_relay_nodes_project ON relay_nodes (project_id, created_at);

-- +goose Down
DROP TABLE IF EXISTS relay_nodes;
