-- +goose Up
-- The sandbox is a PROJECT's, so what bounds it belongs to the plan and
-- to the project - not to the installation.
--
-- Both were platform settings, so every tenant on the box got the same
-- ring buffer whatever they paid, and no project could pick a shorter
-- window than the operator's.
--
--   plans.max_sandbox_messages       - the cap, what a plan sells
--   plans.max_sandbox_retention_days - a CEILING, not the window
--   projects.sandbox_retention_days  - the project's choice, clamped
--
-- 0 means "not set" as on every other limit: unlimited on a plan, and
-- on a project inherit the platform default.
ALTER TABLE plans ADD COLUMN max_sandbox_messages INTEGER NOT NULL DEFAULT 0;
ALTER TABLE plans ADD COLUMN max_sandbox_retention_days INTEGER NOT NULL DEFAULT 0;
ALTER TABLE projects ADD COLUMN sandbox_retention_days INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE plans DROP COLUMN IF EXISTS max_sandbox_messages;
ALTER TABLE plans DROP COLUMN IF EXISTS max_sandbox_retention_days;
ALTER TABLE projects DROP COLUMN IF EXISTS sandbox_retention_days;
