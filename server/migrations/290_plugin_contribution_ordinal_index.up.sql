CREATE UNIQUE INDEX CONCURRENTLY idx_plugin_contribution_release_ordinal
    ON plugin_contribution (release_id, ordinal);
