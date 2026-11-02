ALTER TABLE channel_task_delivery
    DROP COLUMN IF EXISTS channel_sender_id;

ALTER TABLE channel_chat_context_generation
    DROP COLUMN IF EXISTS last_message_id,
    DROP COLUMN IF EXISTS last_thread_id,
    DROP COLUMN IF EXISTS last_sender_id;
