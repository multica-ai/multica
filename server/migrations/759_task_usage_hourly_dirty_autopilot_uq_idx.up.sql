CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_task_usage_hourly_dirty_autopilot_key_idx
    ON task_usage_hourly_dirty (bucket_hour, workspace_id, runtime_id, agent_id, project_id, squad_id, autopilot_id, provider, model)
    NULLS NOT DISTINCT;
