-- 116_channels.down: Remove channel tables and issue columns
ALTER TABLE issue DROP COLUMN IF EXISTS discussion_snapshot;
ALTER TABLE issue DROP COLUMN IF EXISTS source_thread_root_id;
ALTER TABLE issue DROP COLUMN IF EXISTS source_channel_id;
DROP TABLE IF EXISTS channel_read_state;
DROP TABLE IF EXISTS channel_message;
DROP TABLE IF EXISTS channel_member;
DROP TABLE IF EXISTS channel;
