CREATE INDEX CONCURRENTLY workflow_node_activation_status_idx ON workflow_node_activation (workspace_id, run_id, status, created_at, id);
