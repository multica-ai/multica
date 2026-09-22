CREATE UNIQUE INDEX CONCURRENTLY workflow_node_activation_identity_idx ON workflow_node_activation (workspace_id, run_id, scope_instance_id, node_id, generation, activation_no);
