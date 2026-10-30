CREATE UNIQUE INDEX CONCURRENTLY idx_plugin_contribution_release_key
    ON plugin_contribution (release_id, contribution_key);
