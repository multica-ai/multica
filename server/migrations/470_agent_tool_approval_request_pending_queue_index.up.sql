CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_tool_approval_request_pending_queue
    ON agent_tool_approval_request (workspace_id, requested_at ASC, id ASC)
    WHERE status = 'pending';
