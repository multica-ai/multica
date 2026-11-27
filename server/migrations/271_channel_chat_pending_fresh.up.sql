ALTER TABLE channel_chat_session_binding
ADD COLUMN pending_fresh BOOLEAN NOT NULL DEFAULT FALSE;
