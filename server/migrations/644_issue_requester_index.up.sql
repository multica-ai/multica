CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workspace_requester ON issue (workspace_id, requester_user_id) WHERE requester_user_id IS NOT NULL;
