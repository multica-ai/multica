-- Extend the pending-chat partial index to deferred retries (MUL-5750).
--
-- The session pending endpoint now treats a deferred retry as a visible head,
-- and the workspace FAB aggregate must use the same in-flight status set.
-- Migration 143's v2 predicate covers only the four immediately runnable or
-- claimed statuses, so PostgreSQL cannot use it for the five-status aggregate.
--
-- Build v3 before dropping v2 so old and new server versions both retain a
-- usable index throughout a rolling deploy. The old four-status queries are a
-- subset of this predicate and can use v3 safely. This file stays a single
-- statement because CREATE INDEX CONCURRENTLY cannot run in a transaction or
-- a multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_chat_pending_v3
    ON agent_task_queue (chat_session_id, created_at DESC)
    WHERE chat_session_id IS NOT NULL
      AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory', 'deferred');
