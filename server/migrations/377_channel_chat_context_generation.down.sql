DROP TABLE IF EXISTS channel_chat_context_generation;

ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS channel_context_revision;

ALTER TABLE chat_message
    DROP COLUMN IF EXISTS channel_context_revision;

ALTER TABLE channel_chat_session_binding
    DROP COLUMN IF EXISTS context_revision;
