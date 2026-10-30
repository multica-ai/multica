CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_runtime_last_seen_at
    ON agent_runtime (last_seen_at);
