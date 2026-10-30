CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_task_code_changes_task
ON agent_task_code_changes (task_id);
