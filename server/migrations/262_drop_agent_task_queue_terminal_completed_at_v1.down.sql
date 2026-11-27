-- Recreate v1 before migration 261's down step removes v2.
-- Keep this as a bare CREATE so runners that do not provide the down-direction
-- cleanup hook fail closed on a leftover INVALID v1 while the valid v2 index
-- remains in place instead of recording the rollback as restored.
CREATE INDEX CONCURRENTLY idx_agent_task_queue_terminal_completed_at
    ON agent_task_queue (completed_at)
    WHERE status IN ('completed', 'failed');
