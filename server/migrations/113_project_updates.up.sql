-- Project Updates: narrative health posts on a project, authored by a member or
-- an agent. The project's current health is derived from the most recent update
-- (see GetLatestUpdatesForProjects). Ordered by created_at; no manual position.
CREATE TABLE project_update (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    health       TEXT NOT NULL CHECK (health IN ('on_track', 'at_risk', 'off_track')),
    body         TEXT NOT NULL DEFAULT '',
    author_type  TEXT NOT NULL CHECK (author_type IN ('member', 'agent')),
    author_id    UUID NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_project_update_project ON project_update(project_id, created_at DESC);
CREATE INDEX idx_project_update_workspace ON project_update(workspace_id);
