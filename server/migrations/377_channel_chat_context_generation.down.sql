DROP TABLE IF EXISTS channel_chat_context_generation;

ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS channel_context_revision;

ALTER TABLE chat_message
    DROP COLUMN IF EXISTS channel_outbound_message_ids;

ALTER TABLE chat_message
    DROP COLUMN IF EXISTS channel_outbound_chat_id;

ALTER TABLE chat_message
    DROP COLUMN IF EXISTS channel_outbound_installation_id;

ALTER TABLE chat_message
    DROP COLUMN IF EXISTS channel_outbound_type;

ALTER TABLE chat_message
    DROP COLUMN IF EXISTS channel_context_revision;

ALTER TABLE channel_chat_session_binding
    DROP COLUMN IF EXISTS context_revision;
