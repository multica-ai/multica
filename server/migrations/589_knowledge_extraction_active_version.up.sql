CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_extraction_active_version_uidx ON knowledge_extraction_run (version_id) WHERE is_active;
