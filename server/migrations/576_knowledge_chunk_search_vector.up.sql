CREATE INDEX CONCURRENTLY IF NOT EXISTS knowledge_chunk_search_vector_gin_idx ON knowledge_chunk USING GIN (search_vector);
