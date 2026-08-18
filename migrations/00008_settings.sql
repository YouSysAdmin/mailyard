-- Platform-wide setting overrides.
--
-- A key with no row resolves to the registry default in
-- internal/models/setting, so this table only ever holds values an
-- administrator actually changed - and a value set back to the
-- default deletes its row rather than storing it again.

-- +goose Up
CREATE TABLE settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    type       TEXT NOT NULL DEFAULT 'string',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE IF EXISTS settings;
