CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_runtime_provider_usage_snapshot_workspace
    ON runtime_provider_usage_snapshot (workspace_id, runtime_id);
