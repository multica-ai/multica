DROP TABLE IF EXISTS github_pull_request_check_run;

ALTER TABLE github_pull_request
    DROP COLUMN IF EXISTS api_mergeable,
    DROP COLUMN IF EXISTS api_merge_state_status,
    DROP COLUMN IF EXISTS checks_rollup_state,
    DROP COLUMN IF EXISTS snapshot_head_sha,
    DROP COLUMN IF EXISTS snapshot_fetched_at;
