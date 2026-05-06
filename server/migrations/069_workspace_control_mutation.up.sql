CREATE TABLE IF NOT EXISTS workspace_control_mutation (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL,
    source_type TEXT NOT NULL,
    source_id TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('update', 'delete')),
    fields TEXT[] NOT NULL DEFAULT '{}',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'applied', 'apply-failed')),
    error TEXT,
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id UUID NOT NULL,
    applied_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_workspace_control_mutation_issue_latest
    ON workspace_control_mutation(issue_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_workspace_control_mutation_pending
    ON workspace_control_mutation(status, created_at)
    WHERE status = 'pending';
