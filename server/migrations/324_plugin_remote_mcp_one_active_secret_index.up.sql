CREATE UNIQUE INDEX CONCURRENTLY idx_plugin_remote_mcp_one_active_secret
    ON plugin_remote_mcp_secret (installation_id, contribution_id)
    WHERE status = 'active';
