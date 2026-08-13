CREATE UNIQUE INDEX CONCURRENTLY idx_plugin_snapshot_workspace_revision
    ON plugin_capability_snapshot (workspace_id, revision);
