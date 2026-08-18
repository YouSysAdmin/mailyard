-- Who may reach a project, and as what.
--
-- Roles are owner > admin > editor > viewer > developer. Developer is
-- not the bottom rung of that ladder, it is a role off to the side:
-- the email sandbox and nothing else. Membership is asked as
-- Role.IsMember(), never as "at least viewer", which was the same
-- question right up until developer appeared below viewer.

-- +goose Up
CREATE TABLE project_members (
    id                     UUID PRIMARY KEY,
    project_id             UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id                UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role       TEXT NOT NULL DEFAULT 'viewer',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (project_id, user_id)
);

CREATE INDEX idx_project_members_user ON project_members(user_id);

-- +goose Down
DROP TABLE IF EXISTS project_members;
