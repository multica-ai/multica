-- Reverse 032: Runtime ownership, visibility, and task approval

-- Cancel any pending_approval tasks first (can't remove status while rows exist)
UPDATE agent_task_queue SET status = 'cancelled' WHERE status = 'pending_approval';

-- Restore original unique index
DROP INDEX IF EXISTS idx_one_pending_task_per_issue;
CREATE UNIQUE INDEX idx_one_pending_task_per_issue
    ON agent_task_queue (issue_id)
    WHERE status IN ('queued', 'dispatched');

-- Restore original status CHECK
ALTER TABLE agent_task_queue DROP CONSTRAINT agent_task_queue_status_check;
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_status_check
    CHECK (status IN ('queued', 'dispatched', 'running', 'completed', 'failed', 'cancelled'));

-- Drop new columns
ALTER TABLE agent_task_queue DROP COLUMN requested_by;
ALTER TABLE agent DROP COLUMN approval_required;
ALTER TABLE agent_runtime DROP CONSTRAINT agent_runtime_visibility_check;
ALTER TABLE agent_runtime DROP COLUMN visibility;
ALTER TABLE agent_runtime DROP COLUMN owner_id;
