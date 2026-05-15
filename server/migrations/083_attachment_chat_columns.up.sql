ALTER TABLE attachment
  ADD COLUMN chat_session_id UUID REFERENCES chat_session(id) ON DELETE CASCADE,
  ADD COLUMN chat_message_id UUID REFERENCES chat_message(id) ON DELETE CASCADE;

CREATE INDEX idx_attachment_chat_session
  ON attachment(chat_session_id)
  WHERE chat_session_id IS NOT NULL;

CREATE INDEX idx_attachment_chat_message
  ON attachment(chat_message_id)
  WHERE chat_message_id IS NOT NULL;
