CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_tool_approval_request_expiry
    ON agent_tool_approval_request (workspace_id, expires_at ASC, id ASC)
    WHERE status IN ('pending', 'approved');
