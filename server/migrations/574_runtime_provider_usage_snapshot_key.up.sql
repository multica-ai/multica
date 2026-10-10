CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_runtime_provider_usage_snapshot_key
    ON runtime_provider_usage_snapshot (runtime_id, provider, window_id);
