CREATE INDEX CONCURRENTLY idx_agent_task_plugin_execution_manifest
    ON agent_task_queue (plugin_execution_manifest_id)
    WHERE plugin_execution_manifest_id IS NOT NULL;
