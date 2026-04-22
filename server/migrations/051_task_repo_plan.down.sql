ALTER TABLE agent_task_queue DROP CONSTRAINT agent_task_queue_status_check;
ALTER TABLE agent_task_queue ADD CONSTRAINT agent_task_queue_status_check
    CHECK (status IN ('queued', 'dispatched', 'running', 'completed', 'failed', 'cancelled'));

ALTER TABLE agent_task_queue
    DROP COLUMN repo_confidence,
    DROP COLUMN target_repo_url;
