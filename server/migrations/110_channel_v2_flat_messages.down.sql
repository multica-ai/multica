DROP INDEX IF EXISTS idx_channel_thread_root_message;
DROP INDEX IF EXISTS idx_channel_message_reply_to;
DROP INDEX IF EXISTS idx_channel_message_channel_toplevel;

ALTER TABLE channel_thread DROP COLUMN IF EXISTS root_message_id;
ALTER TABLE channel_message DROP COLUMN IF EXISTS reply_to_id;

-- Restore NOT NULL on thread_id (requires removing NULL rows first).
DELETE FROM channel_message WHERE thread_id IS NULL;
ALTER TABLE channel_message ALTER COLUMN thread_id SET NOT NULL;
