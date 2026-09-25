CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_plugin_invocation_installation_created
    ON plugin_invocation (installation_id, created_at DESC);
