CREATE TABLE memory_entry (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    scope_type TEXT NOT NULL CHECK (scope_type IN ('workspace', 'project', 'agent', 'issue')),
    scope_id UUID,
    title TEXT NOT NULL,
    content TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    source_issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
    source_comment_id UUID REFERENCES comment(id) ON DELETE SET NULL,
    proposed_by_type TEXT NOT NULL CHECK (proposed_by_type IN ('member', 'agent')),
    proposed_by_id UUID NOT NULL,
    reviewed_by_type TEXT CHECK (reviewed_by_type IN ('member', 'agent')),
    reviewed_by_id UUID,
    review_note TEXT,
    guardrail JSONB NOT NULL DEFAULT '{}',
    approved_at TIMESTAMPTZ,
    rejected_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (scope_type = 'workspace' AND scope_id IS NULL)
        OR (scope_type <> 'workspace' AND scope_id IS NOT NULL)
    ),
    CHECK (
        (status = 'pending' AND approved_at IS NULL AND rejected_at IS NULL)
        OR (status = 'approved' AND approved_at IS NOT NULL AND rejected_at IS NULL)
        OR (status = 'rejected' AND approved_at IS NULL AND rejected_at IS NOT NULL)
    )
);

CREATE INDEX idx_memory_entry_workspace_status ON memory_entry(workspace_id, status, updated_at DESC);
CREATE INDEX idx_memory_entry_scope ON memory_entry(workspace_id, scope_type, scope_id, status);
CREATE INDEX idx_memory_entry_source_issue ON memory_entry(source_issue_id);
CREATE INDEX idx_memory_entry_source_comment ON memory_entry(source_comment_id);
