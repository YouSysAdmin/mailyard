-- Two header lists a project keeps.
--
-- default_headers_json is a JSON object of custom headers laid under
-- every message the project sends. A message or campaign naming the
-- same header wins, so a default is what goes out when nobody said
-- otherwise.
--
-- submission_drop_headers_json is a JSON array of header names the SMTP
-- submission listener strips before forwarding a client's message. An
-- SMTP client adds X-Mailer and its kin on its own, and this is where a
-- project says it does not want them delivered.
--
-- Both empty means none.

-- +goose Up
ALTER TABLE projects ADD COLUMN default_headers_json TEXT NOT NULL DEFAULT '';
ALTER TABLE projects ADD COLUMN submission_drop_headers_json TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE projects DROP COLUMN IF EXISTS submission_drop_headers_json;
ALTER TABLE projects DROP COLUMN IF EXISTS default_headers_json;
