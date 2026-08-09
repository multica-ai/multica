CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_chat_pending_v4
    ON agent_task_queue (chat_session_id, priority DESC, created_at ASC, id ASC)
    WHERE chat_session_id IS NOT NULL
      AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred', 'waiting_runtime');
