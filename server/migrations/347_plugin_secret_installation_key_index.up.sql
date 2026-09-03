CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_plugin_secret_installation_key
    ON plugin_secret (installation_id, key);
