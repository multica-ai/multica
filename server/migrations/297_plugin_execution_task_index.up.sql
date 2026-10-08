CREATE UNIQUE INDEX CONCURRENTLY idx_plugin_execution_manifest_task
    ON plugin_execution_manifest (task_id);
