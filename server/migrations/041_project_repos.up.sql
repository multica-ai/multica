-- Many-to-many link between projects and workspace repos.
-- repo_id is a soft reference to workspace.repos[].id (a text UUID stored in JSONB).
-- Orphan cleanup (when a repo is removed from workspace.repos) is handled at the
-- application layer from the UpdateWorkspace handler.

CREATE TABLE project_repo (
    project_id UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    repo_id    TEXT NOT NULL,
    position   INT  NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, repo_id)
);

CREATE INDEX idx_project_repo_project ON project_repo(project_id);
CREATE INDEX idx_project_repo_repo ON project_repo(repo_id);
