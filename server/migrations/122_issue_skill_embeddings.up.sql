CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE issue_skill_source (
    skill_id UUID PRIMARY KEY REFERENCES skill(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL UNIQUE REFERENCES issue(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    created_by UUID NOT NULL REFERENCES "user"(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_issue_skill_source_workspace_issue
    ON issue_skill_source(workspace_id, issue_id);

CREATE TABLE skill_embedding (
    skill_id UUID PRIMARY KEY REFERENCES skill(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    embedding vector(1536) NOT NULL,
    embedding_model TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_skill_embedding_workspace
    ON skill_embedding(workspace_id);

CREATE INDEX idx_skill_embedding_vector_cosine
    ON skill_embedding USING hnsw (embedding vector_cosine_ops);
