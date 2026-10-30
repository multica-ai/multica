-- Recreate v1 before migration 257's down step removes v2.
-- Do not use IF NOT EXISTS here: a leftover INVALID v1 must fail closed while
-- the valid v2 index remains in place instead of being recorded as restored.
CREATE UNIQUE INDEX CONCURRENTLY idx_one_pending_task_per_issue_agent
    ON agent_task_queue (issue_id, agent_id)
    WHERE status IN ('queued', 'dispatched');
