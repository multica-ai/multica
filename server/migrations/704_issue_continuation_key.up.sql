CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_continuation_key ON issue_continuation (workspace_id, issue_id, chain_id, remainder_id);
