CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS knowledge_embedding_index_chunk_uidx ON knowledge_embedding (index_id, chunk_id);
