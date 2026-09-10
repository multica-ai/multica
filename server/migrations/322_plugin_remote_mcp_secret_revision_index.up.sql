CREATE UNIQUE INDEX CONCURRENTLY idx_plugin_remote_mcp_secret_revision
    ON plugin_remote_mcp_secret (installation_id, contribution_id, version);
