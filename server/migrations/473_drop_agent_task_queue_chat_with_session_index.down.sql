-- Restore the narrower index before rollback removes migration 472's broader
-- replacement.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_chat_with_session_created_at
ON agent_task_queue (chat_session_id, created_at DESC)
WHERE chat_session_id IS NOT NULL
  AND session_id IS NOT NULL;
