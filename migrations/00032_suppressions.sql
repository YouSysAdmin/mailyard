-- Addresses this project must not send to.
--
-- Permanent by design and never pruned, so on a busy install this
-- reaches millions of rows and stays there. Both indexes below exist
-- because of that: one for the "is this one address blocked" search
-- the console actually asks, one for the keyset page.

-- +goose Up
CREATE TABLE suppressions (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    -- Stored lowercased bare address.
    email               TEXT NOT NULL,
    kind                TEXT NOT NULL DEFAULT 'hard',
    reason              TEXT NOT NULL DEFAULT '',
    -- Which opt-out scope this block belongs to. Empty is a global
    -- block. The list is part of the unique key so an address can be
    -- globally blocked AND separately opted out of one list.
    unsubscribe_list_id    UUID,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (project_id, email, unsubscribe_list_id)
);

-- Serves the send-time filter and the prefix-anchored search.
CREATE INDEX idx_suppressions_proj_email ON suppressions(project_id, email);

-- The keyset page. (project_id, created_at DESC, id DESC) rather than
-- plain created_at, because the cursor is the PAIR: created_at alone
-- is not a total order - two rows written in the same microsecond tie
-- - and a page boundary landing between tied rows either repeats one
-- or loses one.
CREATE INDEX idx_suppressions_proj_created
    ON suppressions (project_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE IF EXISTS suppressions;
