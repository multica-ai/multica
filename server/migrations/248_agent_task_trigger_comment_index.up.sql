CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_trigger_comment_id
    ON agent_task_queue(trigger_comment_id)
    WHERE trigger_comment_id IS NOT NULL;
