ALTER TABLE github_pending_check_suite
    DROP CONSTRAINT github_pending_check_suite_pkey,
    ADD PRIMARY KEY (workspace_id, repo_owner, repo_name, pr_number, suite_id);
ALTER TABLE github_pending_check_suite
    DROP COLUMN provider,
    DROP COLUMN base_host;

ALTER TABLE github_pull_request
    DROP CONSTRAINT github_pull_request_identity_key,
    ADD CONSTRAINT github_pull_request_workspace_id_repo_owner_repo_name_pr_nu_key
        UNIQUE (workspace_id, repo_owner, repo_name, pr_number);
ALTER TABLE github_pull_request
    DROP COLUMN provider,
    DROP COLUMN base_host;
