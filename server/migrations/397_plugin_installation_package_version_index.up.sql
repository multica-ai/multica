-- Deleting a published version has to know whether any workspace still runs it.
-- Without this index that check is a sequential scan on every publish page load.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_plugin_installation_package_version
    ON plugin_installation (package_version_id);
