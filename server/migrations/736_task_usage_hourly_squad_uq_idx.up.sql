CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_task_usage_hourly_squad_key_idx
    ON task_usage_hourly (bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, provider, model)
    NULLS NOT DISTINCT;
