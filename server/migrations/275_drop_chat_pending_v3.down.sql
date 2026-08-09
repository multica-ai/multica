CREATE INDEX CONCURRENTLY idx_agent_task_queue_chat_pending_v3
    ON agent_task_queue (chat_session_id, created_at DESC)
    WHERE chat_session_id IS NOT NULL
      AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred');
