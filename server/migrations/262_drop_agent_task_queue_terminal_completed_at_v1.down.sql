-- Recreate v1 before migration 261's down step removes v2.
-- Do not use IF NOT EXISTS here: pre-migration hooks only run in the `up`
-- direction, so a leftover INVALID v1 must fail closed while the valid v2
-- index remains in place instead of being recorded as restored.
CREATE INDEX CONCURRENTLY idx_agent_task_queue_terminal_completed_at
    ON agent_task_queue (completed_at)
    WHERE status IN ('completed', 'failed');
