CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workspace_member_creator ON issue (workspace_id, creator_id) WHERE creator_type = 'member';
