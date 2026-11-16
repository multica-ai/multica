DROP INDEX IF EXISTS idx_agent_task_queue_escalation_for;
DROP INDEX IF EXISTS idx_agent_task_queue_deferred_fire;

UPDATE agent_task_queue
SET status = 'cancelled',
    completed_at = COALESCE(completed_at, now())
WHERE status = 'deferred';

ALTER TABLE agent_task_queue
    DROP CONSTRAINT agent_task_queue_status_check,
    ADD CONSTRAINT agent_task_queue_status_check
        CHECK (status IN (
            'queued',
            'dispatched',
            'running',
            'completed',
            'failed',
            'cancelled',
            'waiting_local_directory'
        ));

ALTER TABLE agent_task_queue
    DROP COLUMN fire_at,
    DROP COLUMN escalation_for_task_id;
