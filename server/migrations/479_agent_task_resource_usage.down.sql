SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS resource_usage;
