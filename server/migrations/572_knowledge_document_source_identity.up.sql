CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_document_source_identity_uidx ON knowledge_document (knowledge_base_id, source_kind, source_identity) WHERE deleted_at IS NULL;
