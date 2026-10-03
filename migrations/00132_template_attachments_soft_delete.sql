-- A template attachment is stored once and messages REFERENCE it, so
-- deleting it, or its template, only marks the row. The bytes stay
-- until retention finds no message that can still need them.
--
-- template_id goes NULL when the template itself is deleted, and the
-- row is kept for the messages that point at it.

-- +goose Up
ALTER TABLE template_attachments ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE template_attachments ALTER COLUMN template_id DROP NOT NULL;
ALTER TABLE template_attachments DROP CONSTRAINT IF EXISTS template_attachments_template_id_fkey;
ALTER TABLE template_attachments ADD CONSTRAINT template_attachments_template_id_fkey
    FOREIGN KEY (template_id) REFERENCES templates(id) ON DELETE SET NULL;
CREATE INDEX idx_template_attachments_deleted ON template_attachments(deleted_at)
    WHERE deleted_at IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_template_attachments_deleted;
DELETE FROM template_attachments WHERE template_id IS NULL;
ALTER TABLE template_attachments DROP CONSTRAINT IF EXISTS template_attachments_template_id_fkey;
ALTER TABLE template_attachments ADD CONSTRAINT template_attachments_template_id_fkey
    FOREIGN KEY (template_id) REFERENCES templates(id) ON DELETE CASCADE;
ALTER TABLE template_attachments ALTER COLUMN template_id SET NOT NULL;
ALTER TABLE template_attachments DROP COLUMN deleted_at;
