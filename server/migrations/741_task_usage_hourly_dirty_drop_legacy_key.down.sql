CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_task_usage_hourly_dirty_key
    ON task_usage_hourly_dirty (bucket_hour, workspace_id, runtime_id, agent_id, project_id, provider, model)
    NULLS NOT DISTINCT;
