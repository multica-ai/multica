-- The reconciler resolves each candidate task to the assistant message it
-- produced, so it looks chat_message up by task_id and role. Without this
-- index that lookup is a chat_message scan per candidate, on the hot message
-- table, once per reconcile window.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_chat_message_task_assistant
    ON chat_message (task_id)
    WHERE role = 'assistant' AND task_id IS NOT NULL;
