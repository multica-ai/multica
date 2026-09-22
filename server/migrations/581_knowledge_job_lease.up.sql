CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_job_lease_idx ON knowledge_job (status, lease_until);
