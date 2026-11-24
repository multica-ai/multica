CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_usage_hourly_workspace_squad_time
    ON task_usage_hourly (workspace_id, squad_id, bucket_hour DESC)
    WHERE squad_id IS NOT NULL;
