CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_waiting_runtime_workspace
    ON agent_task_queue (placement_workspace_id, priority DESC, created_at ASC, id ASC)
    WHERE status = 'waiting_runtime';
