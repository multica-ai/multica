CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_document_current_version_idx ON knowledge_document (knowledge_base_id, current_version_id) WHERE deleted_at IS NULL;
