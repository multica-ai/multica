CREATE UNIQUE INDEX CONCURRENTLY workflow_scope_instance_identity_idx ON workflow_scope_instance (workspace_id, run_id, definition_scope_id, parent_scope_id, generation) NULLS NOT DISTINCT;
