CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_agent_active
    ON agent_task_queue (agent_id, status)
    WHERE status IN ('queued', 'dispatched', 'running', 'waiting_local_directory');
