CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_maintenance_job_idempotency ON maintenance_job (idempotency_key);
