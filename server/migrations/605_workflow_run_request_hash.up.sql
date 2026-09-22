CREATE INDEX CONCURRENTLY workflow_run_request_hash_idx ON workflow_run (workflow_id, creator_id, request_hash);
