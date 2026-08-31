CREATE TABLE github_pr_poll_cursor (
    workspace_id UUID NOT NULL,
    repo_owner TEXT NOT NULL,
    repo_name TEXT NOT NULL,
    cursor_updated_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, repo_owner, repo_name)
);
