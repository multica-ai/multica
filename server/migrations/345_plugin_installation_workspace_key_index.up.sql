CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_plugin_installation_workspace_key
    ON plugin_installation (workspace_id, plugin_key);
