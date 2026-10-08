CREATE UNIQUE INDEX CONCURRENTLY idx_plugin_installation_config_contribution_revision
    ON plugin_installation_config (installation_id, contribution_id, revision);
