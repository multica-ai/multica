CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_runtime_capacity
    ON agent_task_queue (runtime_id)
    WHERE status IN ('queued', 'deferred', 'dispatched', 'running', 'waiting_local_directory');
