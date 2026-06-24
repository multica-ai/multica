CREATE TABLE knowledge_candidate (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    comment_id UUID REFERENCES comment(id) ON DELETE SET NULL,
    agent_task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    source_type TEXT NOT NULL CHECK (source_type IN ('issue', 'comment', 'agent_task')),
    source_id UUID NOT NULL,
    trigger_reason TEXT NOT NULL,
    signal_strength TEXT NOT NULL CHECK (signal_strength IN ('none', 'weak', 'strong', 'manual')),
    signals TEXT[] NOT NULL DEFAULT '{}',
    score INT NOT NULL DEFAULT 0 CHECK (score >= 0 AND score <= 100),
    status TEXT NOT NULL CHECK (status IN ('pending', 'accepted', 'rejected', 'drafted')),
    dedupe_key TEXT NOT NULL,
    created_by UUID REFERENCES member(id) ON DELETE SET NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    evaluated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, dedupe_key)
);

CREATE INDEX idx_knowledge_candidate_workspace_status
    ON knowledge_candidate(workspace_id, status, score DESC, updated_at DESC);
CREATE INDEX idx_knowledge_candidate_issue
    ON knowledge_candidate(workspace_id, issue_id, updated_at DESC);
CREATE INDEX idx_knowledge_candidate_comment
    ON knowledge_candidate(workspace_id, comment_id)
    WHERE comment_id IS NOT NULL;
CREATE INDEX idx_knowledge_candidate_task
    ON knowledge_candidate(workspace_id, agent_task_id)
    WHERE agent_task_id IS NOT NULL;
