-- Rewritten links, with their click tallies.
--
-- Keyed (project_id, campaign_id, hash), campaign id EMPTY for a
-- transactional send. Tracking used to key on campaign_messages.id,
-- which is why it could not exist outside a campaign - everything
-- keys on emails.id now.
--
-- campaign_id is a plain grouping key with no foreign key, since an
-- empty string satisfies none, and campaign deletion clears these rows
-- explicitly. Scoping the hash keeps one URL in two campaigns as two
-- tallies.

-- +goose Up
CREATE TABLE tracked_links (
    id                     UUID PRIMARY KEY,
    project_id             UUID,
    campaign_id            UUID,
    original_url TEXT NOT NULL,
    -- Stable hash of the URL within its scope, the /tracking/click/
    -- path segment.
    hash         TEXT NOT NULL,
    click_count  INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_tracked_links_key ON tracked_links (project_id, campaign_id, hash);

-- +goose Down
DROP TABLE IF EXISTS tracked_links;
