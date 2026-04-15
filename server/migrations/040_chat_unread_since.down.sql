DROP INDEX IF EXISTS idx_agent_task_queue_chat_pending;
ALTER TABLE chat_session DROP COLUMN unread_since;
