-- Gitee PR webhook integration: adds provider column to github_pull_request,
-- makes installation_id nullable, and creates gitee_webhook_config table.

-- 1. Add provider column with default 'github'
ALTER TABLE github_pull_request
    ADD COLUMN provider TEXT NOT NULL DEFAULT 'github'
        CHECK (provider IN ('github', 'gitee'));

-- 2. Make installation_id nullable (Gitee PRs have no installation)
ALTER TABLE github_pull_request
    ALTER COLUMN installation_id DROP NOT NULL;

-- 3. Drop existing unique constraint and rebuild including provider
ALTER TABLE github_pull_request
    DROP CONSTRAINT IF EXISTS github_pull_request_workspace_id_repo_owner_repo_name_pr_num_key;

ALTER TABLE github_pull_request
    ADD CONSTRAINT github_pull_request_provider_workspace_repo_pr_key
        UNIQUE (provider, workspace_id, repo_owner, repo_name, pr_number);

-- 4. Gitee webhook config table
CREATE TABLE gitee_webhook_config (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    repo_owner   TEXT NOT NULL,
    repo_name    TEXT NOT NULL,
    secret       TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, repo_owner, repo_name)
);

CREATE INDEX idx_gitee_webhook_config_workspace ON gitee_webhook_config(workspace_id);
CREATE INDEX idx_gitee_webhook_config_repo ON gitee_webhook_config(repo_owner, repo_name);
