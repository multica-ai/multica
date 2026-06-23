DROP INDEX IF EXISTS idx_knowledge_embedding_vector_cosine;

ALTER TABLE knowledge_embedding
    RENAME COLUMN embedding TO embedding_1536;

ALTER TABLE knowledge_embedding
    ADD COLUMN dimension INT NOT NULL DEFAULT 1536,
    ADD COLUMN embedding_3072 vector(3072),
    ADD COLUMN embedding_1024 vector(1024),
    ADD COLUMN embedding_768 vector(768);

ALTER TABLE knowledge_embedding
    ALTER COLUMN embedding_1536 DROP NOT NULL;

ALTER TABLE knowledge_embedding
    DROP CONSTRAINT IF EXISTS knowledge_embedding_knowledge_item_id_provider_model_content_hash_key;

ALTER TABLE knowledge_embedding
    ADD CONSTRAINT knowledge_embedding_supported_dimension_check
    CHECK (dimension IN (1536, 3072, 1024, 768)),
    ADD CONSTRAINT knowledge_embedding_dimension_vector_check
    CHECK (
        (dimension = 1536 AND embedding_1536 IS NOT NULL AND embedding_3072 IS NULL AND embedding_1024 IS NULL AND embedding_768 IS NULL)
        OR (dimension = 3072 AND embedding_1536 IS NULL AND embedding_3072 IS NOT NULL AND embedding_1024 IS NULL AND embedding_768 IS NULL)
        OR (dimension = 1024 AND embedding_1536 IS NULL AND embedding_3072 IS NULL AND embedding_1024 IS NOT NULL AND embedding_768 IS NULL)
        OR (dimension = 768 AND embedding_1536 IS NULL AND embedding_3072 IS NULL AND embedding_1024 IS NULL AND embedding_768 IS NOT NULL)
    ),
    ADD CONSTRAINT knowledge_embedding_item_provider_model_dimension_hash_key
    UNIQUE (knowledge_item_id, provider, model, dimension, content_hash);

CREATE INDEX idx_knowledge_embedding_vector_1536_cosine
    ON knowledge_embedding USING ivfflat (embedding_1536 vector_cosine_ops)
    WITH (lists = 100)
    WHERE dimension = 1536 AND embedding_1536 IS NOT NULL;

CREATE INDEX idx_knowledge_embedding_vector_1024_cosine
    ON knowledge_embedding USING ivfflat (embedding_1024 vector_cosine_ops)
    WITH (lists = 100)
    WHERE dimension = 1024 AND embedding_1024 IS NOT NULL;

CREATE INDEX idx_knowledge_embedding_vector_768_cosine
    ON knowledge_embedding USING ivfflat (embedding_768 vector_cosine_ops)
    WITH (lists = 100)
    WHERE dimension = 768 AND embedding_768 IS NOT NULL;
