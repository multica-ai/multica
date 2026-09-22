CREATE UNIQUE INDEX CONCURRENTLY workflow_node_attempt_idx ON workflow_node_run (run_id, node_id, attempt);
