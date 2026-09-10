ALTER TABLE channel_task_delivery
    DROP COLUMN IF EXISTS channel_sender_id;

ALTER TABLE channel_chat_session_binding
    DROP COLUMN IF EXISTS last_sender_id;
