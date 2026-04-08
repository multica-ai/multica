-- GitHub App installations per workspace
CREATE TABLE github_installation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    installation_id BIGINT NOT NULL,
    account_login TEXT NOT NULL,
    account_type TEXT NOT NULL CHECK (account_type IN ('User', 'Organization')),
    app_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, installation_id)
);

CREATE INDEX idx_github_installation_workspace ON github_installation(workspace_id);
CREATE INDEX idx_github_installation_id ON github_installation(installation_id);

-- Pull requests linked to issues
CREATE TABLE issue_pull_request (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    repo_owner TEXT NOT NULL,
    repo_name TEXT NOT NULL,
    pr_number INT NOT NULL,
    title TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'draft', 'merged', 'closed')),
    author TEXT NOT NULL,
    url TEXT NOT NULL,
    branch TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(issue_id, repo_owner, repo_name, pr_number)
);

CREATE INDEX idx_issue_pr_issue ON issue_pull_request(issue_id);
CREATE INDEX idx_issue_pr_workspace ON issue_pull_request(workspace_id);
CREATE INDEX idx_issue_pr_repo ON issue_pull_request(repo_owner, repo_name, pr_number);
