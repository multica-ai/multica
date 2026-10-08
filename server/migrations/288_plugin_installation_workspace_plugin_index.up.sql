CREATE UNIQUE INDEX CONCURRENTLY idx_plugin_installation_workspace_plugin_active
    ON plugin_installation (workspace_id, plugin_id)
    WHERE uninstalled_at IS NULL;
