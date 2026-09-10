-- The version list a publisher reads: newest first, per package.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_plugin_package_version_package
    ON plugin_package_version (package_id, created_at DESC);
