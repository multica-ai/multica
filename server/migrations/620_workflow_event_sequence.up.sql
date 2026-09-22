CREATE UNIQUE INDEX CONCURRENTLY workflow_event_sequence_idx ON workflow_event (run_id, sequence);
