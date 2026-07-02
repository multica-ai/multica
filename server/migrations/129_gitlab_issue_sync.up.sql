CREATE TABLE gitlab_issue (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    connection_id        UUID NOT NULL REFERENCES gitlab_connection(id) ON DELETE CASCADE,
    project_path         TEXT NOT NULL,
    gl_issue_iid         INTEGER NOT NULL,
    gl_project_id        BIGINT NOT NULL,
    issue_id             UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    gl_assignee_username TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, project_path, gl_issue_iid)
);

CREATE INDEX idx_gitlab_issue_workspace ON gitlab_issue(workspace_id);
CREATE INDEX idx_gitlab_issue_issue ON gitlab_issue(issue_id);

ALTER TABLE comment ADD COLUMN gitlab_note_id BIGINT;
CREATE UNIQUE INDEX idx_comment_gitlab_note ON comment(gitlab_note_id) WHERE gitlab_note_id IS NOT NULL;
