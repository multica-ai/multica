CREATE INDEX CONCURRENTLY idx_plugin_installation_config_workspace
    ON plugin_installation_config (workspace_id, installation_id, contribution_id, revision DESC);
