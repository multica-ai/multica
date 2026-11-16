-- Migration 472's index has the same ordered keys and a broader predicate, so
-- the cancelled-chat pointer lookup remains covered without carrying both
-- indexes on the hottest write table.
DROP INDEX CONCURRENTLY IF EXISTS idx_agent_task_queue_chat_with_session_created_at;
