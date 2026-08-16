DROP INDEX IF EXISTS idx_agent_action_log_task_message;

ALTER TABLE agent_action_log
    DROP COLUMN IF EXISTS message_seq,
    DROP COLUMN IF EXISTS task_id;
