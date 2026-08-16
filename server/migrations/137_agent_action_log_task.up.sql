-- Phase 3 control plane: make tool audit rows idempotent per task message.
ALTER TABLE agent_action_log
    ADD COLUMN IF NOT EXISTS task_id TEXT,
    ADD COLUMN IF NOT EXISTS message_seq INTEGER;

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_action_log_task_message
    ON agent_action_log (task_id, message_seq)
    WHERE task_id IS NOT NULL AND message_seq IS NOT NULL;
