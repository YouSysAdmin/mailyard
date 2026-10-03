-- When the retention sweep first found a builder image used by no
-- template. NULL while a template uses it or before the sweep has
-- looked. The sweep deletes an image once this is older than the
-- email log window.

-- +goose Up
ALTER TABLE template_assets ADD COLUMN unreferenced_since TIMESTAMPTZ;

-- +goose Down
ALTER TABLE template_assets DROP COLUMN IF EXISTS unreferenced_since;
