DROP INDEX IF EXISTS idx_attachment_chat_message;
ALTER TABLE attachment DROP COLUMN IF EXISTS chat_message_id;
