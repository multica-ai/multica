CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS agent_tool_approval_request_task_idempotency_uidx
    ON agent_tool_approval_request (task_id, idempotency_key);
