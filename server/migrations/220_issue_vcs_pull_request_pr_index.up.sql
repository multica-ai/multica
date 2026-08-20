-- Single statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or share a multi-command migration file.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_vcs_pull_request_pr
    ON issue_vcs_pull_request (pull_request_id);
