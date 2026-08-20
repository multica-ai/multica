CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_deferred_branch
    ON agent_task_queue (issue_id, agent_id, created_at, id)
    WHERE status = 'deferred' AND branch_context IS NOT NULL;
