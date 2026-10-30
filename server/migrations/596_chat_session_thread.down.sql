ALTER TABLE chat_session
    DROP COLUMN IF EXISTS root_chat_message_id,
    DROP COLUMN IF EXISTS parent_chat_session_id;
