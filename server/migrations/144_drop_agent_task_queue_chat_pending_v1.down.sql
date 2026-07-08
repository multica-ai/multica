-- Recreate the original three-status partial index from migration 040.
-- Single-statement CONCURRENTLY per repo convention.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_chat_pending
    ON agent_task_queue (chat_session_id, created_at DESC)
    WHERE chat_session_id IS NOT NULL
      AND status IN ('queued', 'dispatched', 'running');
