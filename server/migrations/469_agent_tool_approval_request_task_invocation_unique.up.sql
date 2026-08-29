ALTER TABLE agent_tool_approval_request
    ADD CONSTRAINT agent_tool_approval_request_task_invocation_key
    UNIQUE USING INDEX agent_tool_approval_request_task_invocation_uidx;
