-- A version is published once. This index is what makes immutability a database
-- rule rather than a convention the service is trusted to keep.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_plugin_package_version_unique
    ON plugin_package_version (package_id, version);
