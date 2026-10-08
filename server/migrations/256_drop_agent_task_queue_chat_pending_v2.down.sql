-- Recreate v2 before migration 255's down step removes v3.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_chat_pending_v2
    ON agent_task_queue (chat_session_id, created_at DESC)
    WHERE chat_session_id IS NOT NULL
      AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory');
