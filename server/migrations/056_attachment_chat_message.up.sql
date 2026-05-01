-- Allow attachments to be linked to a chat message in addition to issues
-- and comments. Existing rows keep issue_id / comment_id, the new column
-- starts NULL so no existing data is touched.
ALTER TABLE attachment
    ADD COLUMN chat_message_id UUID REFERENCES chat_message(id) ON DELETE CASCADE;

CREATE INDEX idx_attachment_chat_message ON attachment(chat_message_id) WHERE chat_message_id IS NOT NULL;
