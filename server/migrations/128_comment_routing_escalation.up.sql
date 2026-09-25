ALTER TABLE agent_task_queue
    ADD COLUMN escalation_for_task_id UUID,
    ADD COLUMN fire_at TIMESTAMPTZ;

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
            'waiting_local_directory',
            'deferred'
        ));

CREATE INDEX idx_agent_task_queue_deferred_fire
    ON agent_task_queue (runtime_id, fire_at)
    WHERE status = 'deferred';

CREATE INDEX idx_agent_task_queue_escalation_for
    ON agent_task_queue (escalation_for_task_id)
    WHERE escalation_for_task_id IS NOT NULL;
