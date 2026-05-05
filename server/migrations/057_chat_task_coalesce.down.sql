-- CEREBRO-PATCH(migration-idempotent-057-chat-task-coalesce): cerebro modification of upstream file
DROP INDEX IF EXISTS agent_task_queue_chat_session_queued_unique;
