CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_issue_history
    ON agent_task_queue (issue_id, created_at DESC, id DESC);
