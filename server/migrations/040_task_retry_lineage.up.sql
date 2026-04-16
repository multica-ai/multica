-- Add retry lineage tracking to agent_task_queue.
-- retried_from_id points to the original failed/cancelled task that was retried.
ALTER TABLE agent_task_queue ADD COLUMN retried_from_id UUID REFERENCES agent_task_queue(id);

-- Index for the double-retry guard: "has this task already been retried?"
CREATE INDEX idx_agent_task_queue_retried_from ON agent_task_queue(retried_from_id) WHERE retried_from_id IS NOT NULL;
