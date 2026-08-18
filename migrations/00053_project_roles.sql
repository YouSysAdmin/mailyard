-- project_groups becomes project_roles: the table took over the word.
--
-- It was a "custom group", an override instead of one of five preset
-- roles. The presets are gone - measured against the router they
-- opened 153, 153, 118, 59 and 10 of the 153 gated routes, nested one
-- inside the next, with owner and admin identical. So there is one
-- kind of role now and the project writes it. Renaming keeps the rows.
--
-- The constraints keep their generated project_groups_* names. No
-- query names a constraint, and renaming could fail on a database
-- where Postgres already uniquified one.

-- +goose Up
ALTER TABLE project_groups RENAME TO project_roles;
ALTER INDEX idx_project_groups_proj RENAME TO idx_project_roles_proj;

-- +goose Down
ALTER INDEX idx_project_roles_proj RENAME TO idx_project_groups_proj;
ALTER TABLE project_roles RENAME TO project_groups;
