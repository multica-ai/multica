-- Partial index over pending deferred finalizations only: the sweeper scans
-- for rows past the grace period, and the pending set is tiny (cancelled
-- started-but-empty chat tasks awaiting a daemon ack).
--
-- Single-statement migration: CREATE INDEX CONCURRENTLY cannot run inside a
-- transaction or a multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_task_chat_finalize_deferred
    ON agent_task_queue (chat_finalize_deferred_at)
    WHERE chat_finalize_deferred_at IS NOT NULL;
