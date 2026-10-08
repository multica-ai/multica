-- Index the task-owned assistant lookup used by quick-action generation.
--
-- GetChatMessageByTaskAssistant and SetChatMessageQuickActionsByTask both find
-- the newest assistant row for one task. The existing task_id index is partial
-- on role = 'user', so neither query can use it and both scan chat_message.
--
-- Single-statement migration: CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction or a multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_message_assistant_task
    ON chat_message (task_id, created_at DESC)
    WHERE role = 'assistant';
