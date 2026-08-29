CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_tool_action_event_retention
    ON agent_tool_action_event (workspace_id, created_at ASC, id ASC);
