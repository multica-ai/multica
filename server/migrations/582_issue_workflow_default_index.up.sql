-- One Default workflow per workspace (581).
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_workflow_workspace_default
    ON issue_workflow (workspace_id) WHERE is_default;
