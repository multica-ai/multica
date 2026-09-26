CREATE INDEX CONCURRENTLY IF NOT EXISTS task_code_change_issue_idx
    ON task_code_change (issue_id, workspace_id, created_at);
