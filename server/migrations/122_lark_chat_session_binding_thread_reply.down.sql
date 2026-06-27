ALTER TABLE lark_chat_session_binding
    DROP COLUMN IF EXISTS last_lark_message_id,
    DROP COLUMN IF EXISTS last_lark_thread_id;
