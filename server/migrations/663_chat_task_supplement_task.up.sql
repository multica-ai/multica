CREATE INDEX CONCURRENTLY idx_chat_task_supplement_task ON chat_task_supplement (task_id, created_at, message_id);
