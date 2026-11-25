-- Apps an issue's runs used: the call chips and the issue sidebar.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_app_tool_call_issue
    ON app_tool_call (issue_id, created_at)
    WHERE issue_id IS NOT NULL;
