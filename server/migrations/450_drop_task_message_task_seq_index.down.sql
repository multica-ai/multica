CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_message_task_id_seq ON task_message (task_id, seq);
