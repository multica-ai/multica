CREATE UNIQUE INDEX CONCURRENTLY idx_plugin_release_version
    ON plugin_release (plugin_id, version);
