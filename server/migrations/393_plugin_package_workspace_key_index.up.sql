-- One package per plugin key per workspace: publishing a new version of an
-- existing key must find the same package, not create a second one.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_plugin_package_workspace_key
    ON plugin_package (workspace_id, plugin_key);
