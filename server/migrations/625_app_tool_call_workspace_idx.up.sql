-- Activity on an app page, newest first.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_app_tool_call_workspace
    ON app_tool_call (workspace_id, toolkit_slug, created_at DESC);
