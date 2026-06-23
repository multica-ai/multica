CREATE TABLE knowledge_embedding_attempt (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    knowledge_item_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending', 'generated', 'failed', 'unavailable', 'skipped')),
    provider TEXT,
    model TEXT,
    dimension INT CHECK (dimension IS NULL OR dimension IN (1536, 3072, 1024, 768)),
    content_hash TEXT,
    error_message TEXT,
    attempted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    embedded_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (knowledge_item_id, workspace_id),
    FOREIGN KEY (knowledge_item_id, workspace_id)
        REFERENCES knowledge_item(id, workspace_id)
        ON DELETE CASCADE
);

CREATE INDEX idx_knowledge_embedding_attempt_workspace_attempted
    ON knowledge_embedding_attempt(workspace_id, attempted_at DESC);
