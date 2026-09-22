CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_message_history
    ON task_message (task_id, seq, id);
