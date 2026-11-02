CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_maintenance_job_active ON maintenance_job (job_type, scope_key) WHERE status IN ('ready', 'paused');
