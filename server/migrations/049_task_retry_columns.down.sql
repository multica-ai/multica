-- Reverse 049_task_retry_columns. Note: rolling back drops the columns
-- on prod too, even though prod had them before this migration ran. Only
-- run the down migration if you also intend to discard the retry feature.

DROP INDEX IF EXISTS idx_agent_task_queue_parent;

ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS last_heartbeat_at,
    DROP COLUMN IF EXISTS failure_reason,
    DROP COLUMN IF EXISTS parent_task_id,
    DROP COLUMN IF EXISTS max_attempts,
    DROP COLUMN IF EXISTS attempt;
