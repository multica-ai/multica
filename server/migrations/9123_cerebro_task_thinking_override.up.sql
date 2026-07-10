-- Cerebro: per-task thinking override for issue workflow steps.
--
-- NULL/empty means the daemon falls back to agent.thinking_level. The daemon
-- validates the value against the selected runtime/model and drops invalid
-- combinations with a warning instead of failing the task.

ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS thinking_override TEXT;
