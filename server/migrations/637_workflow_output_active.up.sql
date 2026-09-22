CREATE UNIQUE INDEX CONCURRENTLY workflow_output_active_idx ON workflow_output (activation_id) WHERE superseded_at IS NULL;
