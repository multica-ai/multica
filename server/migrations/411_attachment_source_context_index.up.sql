CREATE INDEX CONCURRENTLY idx_attachment_source_context ON attachment (source_context_id) WHERE source_context_id IS NOT NULL;
