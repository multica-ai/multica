-- 117_channel_message_task_id.down: remove task linkage from channel messages.
DROP INDEX IF EXISTS idx_channel_message_task;
ALTER TABLE channel_message DROP COLUMN IF EXISTS task_id;
