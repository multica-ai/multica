CREATE INDEX CONCURRENTLY IF NOT EXISTS plugin_remote_mcp_oauth_state_expiry_idx
    ON plugin_remote_mcp_oauth_state (expires_at);
