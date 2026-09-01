-- Keep the 30-second global liveness sweep off agent_runtime's offline
-- history. Only online rows receive heartbeat writes, and that set is small;
-- the partial index preserves the range predicate without restoring the
-- retired full-table last_seen_at index.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_runtime_online_last_seen
    ON agent_runtime (last_seen_at)
    WHERE status = 'online';
