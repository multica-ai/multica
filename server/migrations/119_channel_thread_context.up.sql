ALTER TABLE channel_message
  ADD COLUMN IF NOT EXISTS thread_id TEXT,
  ADD COLUMN IF NOT EXISTS trigger_depth INTEGER NOT NULL DEFAULT 0;

ALTER TABLE chat_message
  ADD COLUMN IF NOT EXISTS thread_id TEXT,
  ADD COLUMN IF NOT EXISTS trigger_depth INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_channel_message_thread
  ON channel_message(channel_id, thread_id, trigger_depth)
  WHERE thread_id IS NOT NULL;
