DROP INDEX IF EXISTS idx_issue_source_thread;
ALTER TABLE issue DROP COLUMN IF EXISTS source_thread_id;
ALTER TABLE issue DROP COLUMN IF EXISTS source_channel_id;
DROP TABLE IF EXISTS channel_message;
DROP TABLE IF EXISTS channel_thread;
DROP TABLE IF EXISTS channel_member;
DROP TABLE IF EXISTS channel;
