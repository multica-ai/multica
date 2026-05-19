-- Reverse Gitee integration migration

DROP TABLE IF EXISTS gitee_webhook_config;

ALTER TABLE github_pull_request
    DROP CONSTRAINT IF EXISTS github_pull_request_provider_workspace_repo_pr_key;

ALTER TABLE github_pull_request
    ADD CONSTRAINT github_pull_request_workspace_id_repo_owner_repo_name_pr_num_key
        UNIQUE (workspace_id, repo_owner, repo_name, pr_number);

ALTER TABLE github_pull_request
    ALTER COLUMN installation_id SET NOT NULL;

ALTER TABLE github_pull_request
    DROP COLUMN IF EXISTS provider;
