-- Serving one file of a version is a point lookup by path, and a bundle must not
-- be able to ship the same path twice.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_plugin_package_file_path
    ON plugin_package_file (version_id, path);
