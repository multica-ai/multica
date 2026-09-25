CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_plugin_hook_schedule_installation_key
    ON plugin_hook_schedule (installation_id, hook_key);
