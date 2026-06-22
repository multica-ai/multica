DROP INDEX IF EXISTS idx_agent_task_queue_channel_message_agent;
DROP INDEX IF EXISTS idx_agent_task_queue_agent_channel_created;

ALTER TABLE agent_task_queue
DROP COLUMN IF EXISTS channel_reply_to_id,
DROP COLUMN IF EXISTS channel_thread_id,
DROP COLUMN IF EXISTS channel_message_id,
DROP COLUMN IF EXISTS channel_id;
