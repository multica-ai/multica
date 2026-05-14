DROP INDEX IF EXISTS idx_attachment_chat_message;
DROP INDEX IF EXISTS idx_attachment_chat_session;
ALTER TABLE attachment
  DROP COLUMN chat_message_id,
  DROP COLUMN chat_session_id;
