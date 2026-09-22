CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_chunk_version_ordinal_uidx ON knowledge_chunk (version_id, ordinal);
