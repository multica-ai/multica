-- agent_task_queue is on the dispatch hot path. Keep this in a dedicated
-- migration because CREATE INDEX CONCURRENTLY cannot share a transaction with
-- the column addition in 179.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_running_heartbeat
    ON agent_task_queue (last_heartbeat_at)
    WHERE status = 'running';
