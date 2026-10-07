-- How much builder image storage a plan sells a project, in bytes
-- summed over its template_assets rows. Bytes rather than a count,
-- because one image is 2 KB and another is 10 MiB, and what a cap is
-- for is the disk. 0 is unlimited, as on every other plan limit.

-- +goose Up
ALTER TABLE plans ADD COLUMN max_template_asset_bytes BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE plans DROP COLUMN IF EXISTS max_template_asset_bytes;
