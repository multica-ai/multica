CREATE INDEX CONCURRENTLY IF NOT EXISTS task_tool_confirmation_issue_idx
    ON task_tool_confirmation (issue_id) WHERE issue_id IS NOT NULL;
