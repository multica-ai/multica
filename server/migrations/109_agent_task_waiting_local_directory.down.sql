-- Revert: drop the waiting_local_directory status and wait_reason column.
-- Any rows currently sitting in waiting_local_directory are first moved back
-- to dispatched so the restored CHECK constraint accepts them.

UPDATE agent_task_queue SET status = 'dispatched', wait_reason = NULL
WHERE status = 'waiting_local_directory';

ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS wait_reason;

ALTER TABLE agent_task_queue DROP CONSTRAINT IF EXISTS agent_task_queue_status_check;
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_status_check
    CHECK (status IN ('queued', 'dispatched', 'running', 'completed', 'failed', 'cancelled'));
