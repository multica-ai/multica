CREATE INDEX CONCURRENTLY workflow_job_due_idx ON workflow_job (status, due_at, id);
