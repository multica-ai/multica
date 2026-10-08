-- Match the pull-request address used by snapshot refreshes without making the
-- GitHub App installation, which is an authorization scope, part of identity.
-- Leading with pr_number prevents the head-SHA lookup, which has no PR number,
-- from preferring this index over its selective head_sha index.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_github_pull_request_pr_owner_repo
    ON github_pull_request (pr_number, repo_owner, repo_name);
