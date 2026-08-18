-- +goose Up
-- A transactional tracked link has no campaign, so campaign_id is
-- NULL - and a plain UNIQUE index treats every NULL as distinct, so
-- the ON CONFLICT in UpsertTrackedLink could never fire for one.
-- Every send would have inserted another row for the same URL, and
-- the click tally would have been spread across them.
--
-- Same rule, same reason as the suppressions key.
DROP INDEX IF EXISTS idx_tracked_links_key;
CREATE UNIQUE INDEX idx_tracked_links_key
    ON tracked_links (project_id, campaign_id, hash) NULLS NOT DISTINCT;

-- +goose Down
DROP INDEX IF EXISTS idx_tracked_links_key;
CREATE UNIQUE INDEX idx_tracked_links_key ON tracked_links (project_id, campaign_id, hash);
