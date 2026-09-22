CREATE INDEX CONCURRENTLY workflow_job_lease_idx ON workflow_job (status, lease_until, id);
