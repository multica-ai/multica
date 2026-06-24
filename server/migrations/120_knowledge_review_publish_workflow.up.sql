ALTER TABLE knowledge_item
    ADD COLUMN updated_by UUID REFERENCES member(id) ON DELETE SET NULL,
    ADD COLUMN deprecated_at TIMESTAMPTZ;

CREATE TABLE knowledge_publish_target (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    knowledge_item_id UUID NOT NULL REFERENCES knowledge_item(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    target_type TEXT NOT NULL CHECK (target_type IN ('rag', 'wiki', 'skill')),
    target_id UUID,
    target_url TEXT,
    target_title TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_by UUID REFERENCES member(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (knowledge_item_id, target_type)
);

CREATE INDEX idx_knowledge_publish_target_item
    ON knowledge_publish_target(knowledge_item_id);
CREATE INDEX idx_knowledge_publish_target_workspace_type
    ON knowledge_publish_target(workspace_id, target_type);

ALTER TABLE knowledge_publish_target
    ADD CONSTRAINT knowledge_publish_target_item_workspace_fk
    FOREIGN KEY (knowledge_item_id, workspace_id)
    REFERENCES knowledge_item(id, workspace_id)
    ON DELETE CASCADE;
