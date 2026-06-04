-- 117_channel_message_task_id: record which agent task produced each channel message.
-- Allows the frontend to show the full tool-call timeline alongside the agent reply.
ALTER TABLE channel_message ADD COLUMN IF NOT EXISTS task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_channel_message_task ON channel_message(task_id) WHERE task_id IS NOT NULL;
