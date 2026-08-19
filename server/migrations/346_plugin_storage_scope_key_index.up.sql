CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_plugin_storage_scope_key
    ON plugin_storage (installation_id, scope_type, scope_id, key);
