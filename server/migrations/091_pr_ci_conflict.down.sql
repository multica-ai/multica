DROP INDEX IF EXISTS idx_github_pr_check_suite_aggregate;
DROP TABLE IF EXISTS github_pull_request_check_suite;

ALTER TABLE github_pull_request
    DROP COLUMN IF EXISTS mergeable_state,
    DROP COLUMN IF EXISTS head_sha;
