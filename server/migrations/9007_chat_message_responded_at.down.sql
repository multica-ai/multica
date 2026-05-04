DROP INDEX IF EXISTS idx_chat_message_session_unresponded;
ALTER TABLE chat_message DROP COLUMN IF EXISTS responded_at;
