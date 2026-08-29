CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_tool_approval_request_agent_history
    ON agent_tool_approval_request (workspace_id, agent_id, requested_at DESC, id DESC);
