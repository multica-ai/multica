-- Phase B (Gitea) foundation: a mirrored PR's identity now carries the provider
-- and instance host, not just the repo address. Existing GitHub rows backfill to
-- provider='github', base_host='github.com'. The uniqueness key gains
-- (provider, base_host) so the same owner/name/number on different providers or
-- self-hosted instances mirror to distinct rows. workspace_id stays in the key:
-- one installation can bind several workspaces, each keeping its own mirror.

ALTER TABLE github_pull_request
    ADD COLUMN provider  TEXT NOT NULL DEFAULT 'github',
    ADD COLUMN base_host TEXT NOT NULL DEFAULT 'github.com';

ALTER TABLE github_pull_request
    DROP CONSTRAINT github_pull_request_workspace_id_repo_owner_repo_name_pr_nu_key,
    ADD CONSTRAINT github_pull_request_identity_key
        UNIQUE (workspace_id, provider, base_host, repo_owner, repo_name, pr_number);

ALTER TABLE github_pending_check_suite
    ADD COLUMN provider  TEXT NOT NULL DEFAULT 'github',
    ADD COLUMN base_host TEXT NOT NULL DEFAULT 'github.com';

ALTER TABLE github_pending_check_suite
    DROP CONSTRAINT github_pending_check_suite_pkey,
    ADD PRIMARY KEY (workspace_id, provider, base_host, repo_owner, repo_name, pr_number, suite_id);
