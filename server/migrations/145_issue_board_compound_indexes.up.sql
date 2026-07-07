CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workspace_project_status_pos ON issue(workspace_id, project_id, status, position);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workspace_project_created ON issue(workspace_id, project_id, created_at DESC);
