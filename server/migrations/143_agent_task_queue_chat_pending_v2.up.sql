-- Fix a partial-index predicate mismatch on agent_task_queue (MUL-4159).
--
-- Migration 040 created idx_agent_task_queue_chat_pending with the predicate
-- `status IN ('queued', 'dispatched', 'running')`. Migration 109 later added a
-- fourth in-flight status, `waiting_local_directory`, and both pending chat
-- queries (GetPendingChatTask, ListPendingChatTasksByCreator) now filter on all
-- four statuses. Postgres only uses a partial index when it can prove the query
-- predicate is a subset of the index predicate; the extra status made the old
-- index an unusable candidate, so those queries degraded to a Seq Scan on
-- agent_task_queue (or a non-optimal FK path), which is the root cause of the
-- high DB load on this path.
--
-- This v2 index restores index coverage by including all four in-flight
-- statuses. The column list (chat_session_id, created_at DESC) is unchanged so
-- it keeps serving the single-session GetPendingChatTask lookup as well.
--
-- The old index is dropped in a separate migration (144) after this one has
-- built and been verified in EXPLAIN, per the repo convention of never mixing
-- multiple CONCURRENTLY statements in one migration. This file must stay
-- single-statement: CREATE INDEX CONCURRENTLY cannot run inside a transaction
-- or multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_chat_pending_v2
    ON agent_task_queue (chat_session_id, created_at DESC)
    WHERE chat_session_id IS NOT NULL
      AND status IN ('queued', 'dispatched', 'running', 'waiting_local_directory');
