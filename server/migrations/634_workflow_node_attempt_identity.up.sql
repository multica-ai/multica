CREATE UNIQUE INDEX CONCURRENTLY workflow_node_attempt_identity_idx ON workflow_node_attempt (activation_id, attempt_no);
