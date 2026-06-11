DROP INDEX IF EXISTS idx_channel_message_thread;

ALTER TABLE chat_message
  DROP COLUMN IF EXISTS trigger_depth,
  DROP COLUMN IF EXISTS thread_id;

ALTER TABLE channel_message
  DROP COLUMN IF EXISTS trigger_depth,
  DROP COLUMN IF EXISTS thread_id;
