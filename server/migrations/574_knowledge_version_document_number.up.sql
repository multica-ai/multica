CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_document_version_document_number_uidx ON knowledge_document_version (document_id, version_number);
