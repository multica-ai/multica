-- Runtime GC orders stale offline candidates by last_seen_at and takes a
-- bounded prefix. Offline heartbeat timestamps are stable, so this partial
-- index removes the full-table sort with negligible steady-state write cost.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_runtime_offline_last_seen
    ON agent_runtime (last_seen_at, id)
    WHERE status = 'offline';
