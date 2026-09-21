CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_comment_agent_delivery_task_pending
    ON comment_agent_delivery (task_id, created_at, comment_id)
    WHERE status IN ('pending', 'steering');
