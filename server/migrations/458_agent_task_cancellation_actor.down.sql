ALTER TABLE agent_task_queue
    DROP COLUMN cancelled_by_name,
    DROP COLUMN cancelled_by_id,
    DROP COLUMN cancelled_by_type;
