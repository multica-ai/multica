CREATE INDEX CONCURRENTLY agent_terminal_session_task_idx ON agent_terminal_session(task_id, updated_at DESC);
