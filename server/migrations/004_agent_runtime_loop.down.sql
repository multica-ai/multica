DROP INDEX IF EXISTS idx_agent_task_queue_runtime_pending;
DROP INDEX IF EXISTS idx_agent_runtime_status;
DROP INDEX IF EXISTS idx_agent_runtime_workspace;

ALTER TABLE agent_task_queue
    DROP CONSTRAINT IF EXISTS agent_task_queue_runtime_id_fkey,
    DROP COLUMN IF EXISTS runtime_id;

ALTER TABLE agent
    DROP CONSTRAINT IF EXISTS agent_runtime_id_fkey,
    DROP COLUMN IF EXISTS runtime_id;

DROP TABLE IF EXISTS agent_runtime;
