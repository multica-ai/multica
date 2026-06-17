ALTER TABLE agent_task_queue
ADD COLUMN channel_id UUID REFERENCES channel(id) ON DELETE SET NULL,
ADD COLUMN channel_message_id UUID REFERENCES channel_message(id) ON DELETE SET NULL,
ADD COLUMN channel_thread_id UUID REFERENCES channel_thread(id) ON DELETE SET NULL,
ADD COLUMN channel_reply_to_id UUID REFERENCES channel_message(id) ON DELETE SET NULL;

CREATE INDEX idx_agent_task_queue_agent_channel_created
    ON agent_task_queue(agent_id, channel_id, created_at DESC)
    WHERE channel_id IS NOT NULL;

CREATE INDEX idx_agent_task_queue_channel_message_agent
    ON agent_task_queue(channel_message_id, agent_id)
    WHERE channel_message_id IS NOT NULL;
