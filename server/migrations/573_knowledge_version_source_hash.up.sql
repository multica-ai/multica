CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_document_version_source_hash_idx ON knowledge_document_version (knowledge_base_id, source_hash);
