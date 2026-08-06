-- Recreate v1 before migration 261's down step removes v2.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_terminal_completed_at
    ON agent_task_queue (completed_at)
    WHERE status IN ('completed', 'failed');
