-- CEREBRO-PATCH(task-delegation-context): JEH-1436 records delegated agent-start provenance on queued tasks.
ALTER TABLE agent_task_queue
    ADD COLUMN IF NOT EXISTS original_user_id UUID REFERENCES "user"(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS delegating_agent_id UUID REFERENCES agent(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS source_task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS delegation_source TEXT;

CREATE INDEX IF NOT EXISTS idx_agent_task_queue_original_user
    ON agent_task_queue(original_user_id)
    WHERE original_user_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_agent_task_queue_source_task
    ON agent_task_queue(source_task_id)
    WHERE source_task_id IS NOT NULL;
