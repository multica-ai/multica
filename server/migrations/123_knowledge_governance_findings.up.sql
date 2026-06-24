CREATE TABLE knowledge_governance_finding (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    knowledge_item_id UUID NOT NULL REFERENCES knowledge_item(id) ON DELETE CASCADE,
    finding_type TEXT NOT NULL CHECK (finding_type IN ('stale', 'conflict', 'low_effectiveness', 'misleading', 'outdated')),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'drafted', 'accepted', 'rejected', 'dismissed', 'archived', 'deprecated')),
    severity INT NOT NULL DEFAULT 0 CHECK (severity >= 0 AND severity <= 100),
    reason TEXT NOT NULL DEFAULT '',
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    suggested_action TEXT NOT NULL DEFAULT '',
    source_map JSONB NOT NULL DEFAULT '{}'::jsonb,
    draft_knowledge_item_id UUID REFERENCES knowledge_item(id) ON DELETE SET NULL,
    resolved_by UUID REFERENCES member(id) ON DELETE SET NULL,
    resolved_at TIMESTAMPTZ,
    dismissed_by UUID REFERENCES member(id) ON DELETE SET NULL,
    dismissed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, knowledge_item_id, finding_type),
    FOREIGN KEY (knowledge_item_id, workspace_id)
        REFERENCES knowledge_item(id, workspace_id)
        ON DELETE CASCADE
);

CREATE INDEX idx_knowledge_governance_finding_queue
    ON knowledge_governance_finding(workspace_id, status, severity DESC, updated_at DESC);

CREATE INDEX idx_knowledge_governance_finding_item
    ON knowledge_governance_finding(workspace_id, knowledge_item_id, updated_at DESC);

ALTER TABLE knowledge_source
    DROP CONSTRAINT IF EXISTS knowledge_source_source_type_check;

ALTER TABLE knowledge_source
    ADD CONSTRAINT knowledge_source_source_type_check
    CHECK (source_type IN ('knowledge', 'issue', 'comment', 'agent_task', 'pull_request', 'commit'));
