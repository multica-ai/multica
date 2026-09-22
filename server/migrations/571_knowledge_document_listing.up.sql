CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_document_listing_idx ON knowledge_document (knowledge_base_id, deleted_at, created_at, id);
