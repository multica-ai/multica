-- Bound AdvanceCancelledChatSessionPointer's newer-task guard to one chat
-- instead of scanning the global task population (MUL-7247).
--
-- This is a net-new index on the hottest write table and may contain most
-- historical chat tasks: session_id is normally present once a chat task has
-- started. The partial predicate still excludes every issue task and chat tasks
-- that never established a provider session.
-- Measure the production build set before rollout with:
-- SELECT count(*) FROM agent_task_queue
-- WHERE chat_session_id IS NOT NULL AND session_id IS NOT NULL;
--
-- Keep this separate from idx_agent_task_queue_chat_pending_v3. That index's
-- status predicate is what keeps workspace-wide pending-task queries selective,
-- while this guard must see both in-flight and terminal tasks with sessions.
--
-- This file must stay single-statement: CREATE INDEX CONCURRENTLY cannot run in
-- a transaction or a multi-command string.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_chat_with_session_created_at
ON agent_task_queue (chat_session_id, created_at DESC)
WHERE chat_session_id IS NOT NULL
  AND session_id IS NOT NULL;
