CREATE UNIQUE INDEX CONCURRENTLY agent_task_queue_execution_uidx ON agent_task_queue (execution_id) WHERE execution_id IS NOT NULL;
