CREATE UNIQUE INDEX CONCURRENTLY workflow_node_attempt_task_idx ON workflow_node_attempt (task_id) WHERE task_id IS NOT NULL;
