CREATE UNIQUE INDEX CONCURRENTLY idx_plugin_grant_revision
    ON plugin_grant (installation_id, capability, grant_revision);
