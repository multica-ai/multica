CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS task_tool_confirmation_task_call_uidx
    ON task_tool_confirmation (task_id, call_id);
