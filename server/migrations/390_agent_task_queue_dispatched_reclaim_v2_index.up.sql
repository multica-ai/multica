-- Put dispatched_at directly after runtime_id so stale-reclaim scans can use
-- both equality and time-range conditions before applying the priority sort.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_dispatched_reclaim_v2
    ON agent_task_queue (runtime_id, dispatched_at ASC, priority DESC)
    WHERE status = 'dispatched' AND started_at IS NULL;
