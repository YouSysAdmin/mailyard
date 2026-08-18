-- tracked_links goes when its campaign or its project goes.
--
-- 00041 said "campaign deletion clears these rows explicitly". Nothing
-- did: there is no DELETE FROM tracked_links anywhere in the tree, and
-- the table had no foreign key, no retention window and no owner. So
-- deleting a campaign or a project orphaned every rewritten link it
-- had, permanently, and the table grew one row per unique URL per
-- campaign for the life of the installation.
--
-- Foreign keys rather than statements in the two handlers, because that
-- is how every other tenant table is cleaned up here and it cannot be
-- forgotten by a third caller.
--
-- BOTH columns stay NULLABLE and that is not an oversight: a
-- transactional link has no campaign, and campaign_id NULL is what the
-- 00061 NULLS NOT DISTINCT key exists to make upsertable. A NULL
-- references nothing, so a nullable foreign key is satisfied by it.
--
-- The DELETE ahead of each constraint is required, not tidying. An
-- installation that has already deleted a campaign holds rows pointing
-- at ids that are gone, and ADD CONSTRAINT validates existing rows - so
-- without this the migration fails on exactly the installations that
-- have the problem. On a fresh database both statements match nothing.

-- +goose Up
DELETE FROM tracked_links
 WHERE campaign_id IS NOT NULL
   AND campaign_id NOT IN (SELECT id FROM campaigns);

DELETE FROM tracked_links
 WHERE project_id IS NOT NULL
   AND project_id NOT IN (SELECT id FROM projects);

ALTER TABLE tracked_links
    ADD CONSTRAINT tracked_links_campaign_id_fkey
    FOREIGN KEY (campaign_id) REFERENCES campaigns(id) ON DELETE CASCADE;

ALTER TABLE tracked_links
    ADD CONSTRAINT tracked_links_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE tracked_links DROP CONSTRAINT IF EXISTS tracked_links_project_id_fkey;
ALTER TABLE tracked_links DROP CONSTRAINT IF EXISTS tracked_links_campaign_id_fkey;
