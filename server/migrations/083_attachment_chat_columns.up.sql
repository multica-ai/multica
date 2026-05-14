-- CEREBRO-PATCH(migration-083-attachment-chat-columns): chat_message_id already exists in fork migration 056.
ALTER TABLE attachment
  ADD COLUMN IF NOT EXISTS chat_session_id UUID REFERENCES chat_session(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_attachment_chat_session
  ON attachment(chat_session_id)
  WHERE chat_session_id IS NOT NULL;
