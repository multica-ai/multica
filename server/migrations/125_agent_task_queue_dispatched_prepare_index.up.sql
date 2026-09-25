-- Covers reclaim scans over dispatched tasks while allowing prepare lease checks.
--
-- agent_task_queue is hot, so this must stay in its own single-statement
-- migration and must use CONCURRENTLY. Keeping CONCURRENTLY isolated avoids
-- Postgres' implicit transaction block for multi-statement query strings.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_dispatched_prepare
    ON agent_task_queue (runtime_id, priority DESC, dispatched_at ASC)
    WHERE status = 'dispatched' AND started_at IS NULL;
