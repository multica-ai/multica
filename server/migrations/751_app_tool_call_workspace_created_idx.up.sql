-- Per-member app tool call usage for a period on the billing page: every
-- toolkit's calls of one workspace in a time range. idx_app_tool_call_workspace
-- leads with toolkit_slug after workspace_id, so it cannot bound the range.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_app_tool_call_workspace_created
    ON app_tool_call (workspace_id, created_at);
