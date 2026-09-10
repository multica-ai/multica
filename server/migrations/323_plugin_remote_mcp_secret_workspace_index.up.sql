CREATE INDEX CONCURRENTLY idx_plugin_remote_mcp_secret_workspace
    ON plugin_remote_mcp_secret (workspace_id, installation_id, contribution_id, version DESC);
