-- Removing this index is functionally safe, but returns
-- AdvanceCancelledChatSessionPointer to a global agent_task_queue scan.
DROP INDEX CONCURRENTLY IF EXISTS idx_agent_task_queue_chat_with_session_created_at;
