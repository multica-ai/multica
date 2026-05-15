-- CEREBRO-PATCH(migration-083-attachment-chat-columns): chat_message_id is owned by fork migration 056.
DROP INDEX IF EXISTS idx_attachment_chat_session;
ALTER TABLE attachment
  DROP COLUMN IF EXISTS chat_session_id;
