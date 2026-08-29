CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_tool_action_event_agent_history
    ON agent_tool_action_event (workspace_id, agent_id, created_at DESC, id DESC);
