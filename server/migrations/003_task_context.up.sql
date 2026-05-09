-- Add context snapshot to agent tasks so daemons have everything needed to execute
ALTER TABLE agent_task_queue
    ADD COLUMN context JSONB;

-- Partial index for efficient daemon polling of pending tasks
CREATE INDEX idx_agent_task_queue_pending
    ON agent_task_queue(agent_id, priority DESC, created_at ASC)
    WHERE status IN ('queued', 'dispatched');

-- Unique constraint for daemon connection upsert
ALTER TABLE daemon_connection
    ADD CONSTRAINT uq_daemon_agent UNIQUE (agent_id, daemon_id);
