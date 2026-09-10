CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_runtime_telemetry_last_seen
    ON agent_runtime (last_seen_at, daemon_id)
    WHERE daemon_id IS NOT NULL;
