-- Every chat_session delete runs the legacy agent_task_queue foreign-key
-- action with only chat_session_id in its predicate. The existing chat indexes
-- add status, session_id, or retired_session_id predicates and cannot serve
-- that lookup, so each deleted session scans the global task table.
--
-- Keep created_at in the key so this broader index also covers migration 465's
-- cancelled-chat pointer lookup. Migration 473 drops that narrower index only
-- after this concurrent build has completed successfully.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_task_queue_chat_session
ON agent_task_queue (chat_session_id, created_at DESC)
WHERE chat_session_id IS NOT NULL;
