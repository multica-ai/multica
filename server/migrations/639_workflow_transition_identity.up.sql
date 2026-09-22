CREATE UNIQUE INDEX CONCURRENTLY workflow_transition_identity_idx ON workflow_transition (source_activation_id, outlet, target_scope_id, generation);
