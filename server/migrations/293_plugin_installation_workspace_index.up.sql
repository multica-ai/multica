CREATE INDEX CONCURRENTLY idx_plugin_installation_workspace
    ON plugin_installation (workspace_id, id);
