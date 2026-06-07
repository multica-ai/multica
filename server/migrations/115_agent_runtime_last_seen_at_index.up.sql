-- Covers sampler reads over runtime heartbeats:
--   * runtime_online: last_seen_at > now() - online window
--   * runtime_heartbeat_age: last_seen_at > now() - 15 minutes ORDER BY last_seen_at DESC
--
-- agent_runtime heartbeat writes are frequent, so this must stay in its own
-- single-statement migration and must use CONCURRENTLY.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_runtime_last_seen_at
    ON agent_runtime (last_seen_at);
