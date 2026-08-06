CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_message_task_assistant
    ON chat_message (task_id)
    WHERE role = 'assistant' AND task_id IS NOT NULL;
