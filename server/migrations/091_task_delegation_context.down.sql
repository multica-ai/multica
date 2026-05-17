-- CEREBRO-PATCH(task-delegation-context): JEH-1436 rollback delegated agent-start provenance on queued tasks.
DROP INDEX IF EXISTS idx_agent_task_queue_source_task;
DROP INDEX IF EXISTS idx_agent_task_queue_original_user;

ALTER TABLE agent_task_queue
    DROP COLUMN IF EXISTS delegation_source,
    DROP COLUMN IF EXISTS source_task_id,
    DROP COLUMN IF EXISTS delegating_agent_id,
    DROP COLUMN IF EXISTS original_user_id;
