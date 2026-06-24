DROP INDEX IF EXISTS idx_knowledge_embedding_vector_768_cosine;
DROP INDEX IF EXISTS idx_knowledge_embedding_vector_1024_cosine;
DROP INDEX IF EXISTS idx_knowledge_embedding_vector_3072_cosine;
DROP INDEX IF EXISTS idx_knowledge_embedding_vector_1536_cosine;

DELETE FROM knowledge_embedding WHERE dimension <> 1536;

ALTER TABLE knowledge_embedding
    DROP CONSTRAINT IF EXISTS knowledge_embedding_item_provider_model_dimension_hash_key,
    DROP CONSTRAINT IF EXISTS knowledge_embedding_dimension_vector_check,
    DROP CONSTRAINT IF EXISTS knowledge_embedding_supported_dimension_check;

ALTER TABLE knowledge_embedding
    DROP COLUMN embedding_768,
    DROP COLUMN embedding_1024,
    DROP COLUMN embedding_3072,
    DROP COLUMN dimension;

ALTER TABLE knowledge_embedding
    ALTER COLUMN embedding_1536 SET NOT NULL;

ALTER TABLE knowledge_embedding
    RENAME COLUMN embedding_1536 TO embedding;

ALTER TABLE knowledge_embedding
    ADD CONSTRAINT knowledge_embedding_knowledge_item_id_provider_model_content_hash_key
    UNIQUE (knowledge_item_id, provider, model, content_hash);

CREATE INDEX idx_knowledge_embedding_vector_cosine
    ON knowledge_embedding USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);
