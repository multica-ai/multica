ALTER TABLE knowledge_retrieval_event
    ADD COLUMN agent_task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    ADD COLUMN query_source TEXT NOT NULL DEFAULT 'interactive'
        CHECK (query_source IN ('interactive', 'task_claim', 'agent_search')),
    ADD COLUMN result_scores JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE knowledge_injection_event
    ADD COLUMN rank INT,
    ADD COLUMN score DOUBLE PRECISION,
    ADD COLUMN injection_reason TEXT,
    ADD COLUMN token_budget INT,
    ADD COLUMN discarded_reason TEXT;

ALTER TABLE knowledge_feedback
    ADD COLUMN agent_task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL;

CREATE TABLE knowledge_usage_event (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    knowledge_item_id UUID NOT NULL REFERENCES knowledge_item(id) ON DELETE CASCADE,
    agent_task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    usage_source TEXT NOT NULL CHECK (usage_source IN ('agent_reference', 'active_search')),
    reference_text TEXT,
    task_status TEXT,
    task_result JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (knowledge_item_id, workspace_id)
        REFERENCES knowledge_item(id, workspace_id)
        ON DELETE CASCADE
);

CREATE INDEX idx_knowledge_retrieval_event_task
    ON knowledge_retrieval_event(agent_task_id)
    WHERE agent_task_id IS NOT NULL;
CREATE INDEX idx_knowledge_feedback_task
    ON knowledge_feedback(agent_task_id)
    WHERE agent_task_id IS NOT NULL;
CREATE INDEX idx_knowledge_usage_event_workspace_created
    ON knowledge_usage_event(workspace_id, created_at DESC);
CREATE INDEX idx_knowledge_usage_event_item
    ON knowledge_usage_event(knowledge_item_id);
CREATE INDEX idx_knowledge_usage_event_task
    ON knowledge_usage_event(agent_task_id)
    WHERE agent_task_id IS NOT NULL;
