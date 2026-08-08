CREATE UNIQUE INDEX CONCURRENTLY idx_platform_extension_release_workspace_key_version
    ON platform_extension_release (workspace_id, extension_key, version);
