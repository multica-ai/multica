CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_telemetry_started
    ON agent_task_queue (started_at)
    WHERE started_at IS NOT NULL;
