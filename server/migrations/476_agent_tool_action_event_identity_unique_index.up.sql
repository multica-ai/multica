CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS agent_tool_action_event_identity_uidx
    ON agent_tool_action_event (workspace_id, task_id, invocation_id, event_type);
