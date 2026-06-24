CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE knowledge_item (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id UUID REFERENCES project(id) ON DELETE SET NULL,
    agent_id UUID REFERENCES agent(id) ON DELETE SET NULL,
    title TEXT NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('lesson', 'playbook', 'reference')),
    domain_labels TEXT[] NOT NULL DEFAULT '{}',
    problem_pattern TEXT NOT NULL DEFAULT '',
    trigger_conditions TEXT NOT NULL DEFAULT '',
    diagnostic_steps TEXT NOT NULL DEFAULT '',
    recommended_practice TEXT NOT NULL DEFAULT '',
    anti_patterns TEXT NOT NULL DEFAULT '',
    applicability TEXT NOT NULL DEFAULT '',
    confidence_status TEXT NOT NULL DEFAULT 'medium' CHECK (confidence_status IN ('low', 'medium', 'high')),
    lifecycle_status TEXT NOT NULL DEFAULT 'draft' CHECK (lifecycle_status IN ('draft', 'reviewed', 'published', 'archived', 'deprecated')),
    created_by UUID REFERENCES member(id) ON DELETE SET NULL,
    reviewed_by UUID REFERENCES member(id) ON DELETE SET NULL,
    reviewed_at TIMESTAMPTZ,
    published_at TIMESTAMPTZ,
    archived_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_source (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    knowledge_item_id UUID NOT NULL REFERENCES knowledge_item(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    source_type TEXT NOT NULL CHECK (source_type IN ('issue', 'comment', 'agent_task', 'pull_request', 'commit')),
    source_id UUID,
    source_url TEXT,
    source_title TEXT,
    source_excerpt TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (source_id IS NOT NULL OR NULLIF(btrim(COALESCE(source_url, '')), '') IS NOT NULL)
);

CREATE TABLE knowledge_embedding (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    knowledge_item_id UUID NOT NULL REFERENCES knowledge_item(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    model TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    embedding vector(1536) NOT NULL,
    embedded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (knowledge_item_id, provider, model, content_hash)
);

CREATE TABLE knowledge_feedback (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    knowledge_item_id UUID NOT NULL REFERENCES knowledge_item(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    member_id UUID REFERENCES member(id) ON DELETE SET NULL,
    value TEXT NOT NULL CHECK (value IN ('helpful', 'not_helpful', 'misleading', 'outdated')),
    note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_retrieval_event (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    member_id UUID REFERENCES member(id) ON DELETE SET NULL,
    query TEXT,
    retrieval_mode TEXT NOT NULL CHECK (retrieval_mode IN ('text', 'vector', 'hybrid')),
    filters JSONB NOT NULL DEFAULT '{}',
    result_count INT NOT NULL DEFAULT 0,
    top_knowledge_item_ids UUID[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE knowledge_injection_event (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    knowledge_item_id UUID NOT NULL REFERENCES knowledge_item(id) ON DELETE CASCADE,
    agent_task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    injection_target TEXT NOT NULL,
    retrieval_event_id UUID REFERENCES knowledge_retrieval_event(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_knowledge_item_workspace_status
    ON knowledge_item(workspace_id, lifecycle_status, updated_at DESC);
CREATE INDEX idx_knowledge_item_project
    ON knowledge_item(workspace_id, project_id)
    WHERE project_id IS NOT NULL;
CREATE INDEX idx_knowledge_item_agent
    ON knowledge_item(workspace_id, agent_id)
    WHERE agent_id IS NOT NULL;
CREATE INDEX idx_knowledge_item_domain_labels
    ON knowledge_item USING GIN(domain_labels);
CREATE INDEX idx_knowledge_source_item
    ON knowledge_source(knowledge_item_id);
CREATE INDEX idx_knowledge_source_workspace_type
    ON knowledge_source(workspace_id, source_type);
CREATE INDEX idx_knowledge_embedding_item
    ON knowledge_embedding(knowledge_item_id);
CREATE INDEX idx_knowledge_embedding_workspace
    ON knowledge_embedding(workspace_id);
CREATE INDEX idx_knowledge_embedding_vector_cosine
    ON knowledge_embedding USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);
CREATE INDEX idx_knowledge_feedback_item
    ON knowledge_feedback(knowledge_item_id);
CREATE INDEX idx_knowledge_retrieval_event_workspace_created
    ON knowledge_retrieval_event(workspace_id, created_at DESC);
CREATE INDEX idx_knowledge_injection_event_workspace_created
    ON knowledge_injection_event(workspace_id, created_at DESC);

ALTER TABLE knowledge_item
    ADD CONSTRAINT knowledge_item_id_workspace_unique UNIQUE (id, workspace_id);

ALTER TABLE knowledge_source
    ADD CONSTRAINT knowledge_source_item_workspace_fk
    FOREIGN KEY (knowledge_item_id, workspace_id)
    REFERENCES knowledge_item(id, workspace_id)
    ON DELETE CASCADE;

ALTER TABLE knowledge_embedding
    ADD CONSTRAINT knowledge_embedding_item_workspace_fk
    FOREIGN KEY (knowledge_item_id, workspace_id)
    REFERENCES knowledge_item(id, workspace_id)
    ON DELETE CASCADE;

ALTER TABLE knowledge_feedback
    ADD CONSTRAINT knowledge_feedback_item_workspace_fk
    FOREIGN KEY (knowledge_item_id, workspace_id)
    REFERENCES knowledge_item(id, workspace_id)
    ON DELETE CASCADE;
