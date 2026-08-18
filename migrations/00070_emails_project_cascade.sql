-- Deleting a project takes its email log with it.
--
-- emails.project_id was the ONE foreign key to projects without an ON
-- DELETE action - 27 tenant tables cascade and this one did not - so
-- "Delete removes a project and everything in it" was false for any
-- project that had ever sent a message. Postgres refused the parent
-- DELETE with 23503, response.Internal turned that into a 500 (a
-- foreign-key violation is not a malformed id, so the MalformedID
-- softening does not see it), and the owner could not delete the
-- project at all.
--
-- Not self-healing either. With retention off (0 means keep forever)
-- the log is never emptied, and a scheduled row is exempt from every
-- purge by design, so waiting did not help.
--
-- Reproduced on a real database before and after: one project plus one
-- email row, DELETE FROM projects refused with
-- "violates foreign key constraint emails_project_id_fkey", and after
-- this change the same DELETE succeeds and leaves zero email rows.
--
-- DROP then ADD on the PARENT reaches every partition - verified, all
-- seven rows in pg_constraint flipped from confdeltype 'a' to 'c', and
-- new partitions inherit it because internal/core/partition creates
-- them with PARTITION OF.
--
-- The ADD revalidates the table, which is a scan. It is one statement
-- on a table whose rows already satisfied the same reference, and the
-- alternative (NOT VALID) is not accepted for a partitioned parent.
--
-- Blobs are NOT handled here, because SQL cannot reach them: an
-- attachment offloaded to the blob store is named only by the row
-- being cascaded away. Project deletion drops them first, in the
-- handler, the same order the retention sweep uses.

-- +goose Up
ALTER TABLE emails DROP CONSTRAINT emails_project_id_fkey;
ALTER TABLE emails ADD CONSTRAINT emails_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE emails DROP CONSTRAINT emails_project_id_fkey;
ALTER TABLE emails ADD CONSTRAINT emails_project_id_fkey
    FOREIGN KEY (project_id) REFERENCES projects(id);
