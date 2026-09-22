CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_job_logical_key_uidx ON knowledge_job (workspace_id, logical_key);
