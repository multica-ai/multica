CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_tool_approval_request_retention
    ON agent_tool_approval_request (workspace_id, (COALESCE(consumed_at, decided_at)) ASC, id ASC)
    WHERE status IN ('consumed', 'denied', 'expired', 'cancelled');
