CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_tool_action_event_dashboard
    ON agent_tool_action_event (workspace_id, event_type, coverage_kind, created_at DESC, id DESC);
