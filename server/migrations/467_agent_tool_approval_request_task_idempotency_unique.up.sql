ALTER TABLE agent_tool_approval_request
    ADD CONSTRAINT agent_tool_approval_request_task_idempotency_key
    UNIQUE USING INDEX agent_tool_approval_request_task_idempotency_uidx;
