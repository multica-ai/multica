CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_pool_deferred_due
    ON agent_task_queue (placement_workspace_id, fire_at ASC, id ASC)
    WHERE runtime_binding_mode = 'pool' AND status = 'deferred';
