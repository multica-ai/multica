CREATE TABLE governance_approval (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    action_id TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id UUID NOT NULL,
    issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
    approval_source_type TEXT NOT NULL CHECK (approval_source_type IN ('issue_comment', 'issue_metadata', 'manual')),
    approval_source_id UUID,
    approved_by_type TEXT NOT NULL CHECK (approved_by_type IN ('member', 'agent')),
    approved_by_id UUID NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_governance_approval_lookup
    ON governance_approval(workspace_id, action_id, target_type, target_id, created_at DESC)
    WHERE consumed_at IS NULL;

CREATE TABLE governance_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    action_id TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id UUID NOT NULL,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member', 'agent')),
    actor_id UUID NOT NULL,
    before_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    after_summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
    approval_id UUID REFERENCES governance_approval(id) ON DELETE SET NULL,
    approval_source_type TEXT,
    approval_source_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_governance_audit_workspace_created
    ON governance_audit(workspace_id, created_at DESC);

CREATE INDEX idx_governance_audit_target
    ON governance_audit(workspace_id, target_type, target_id, created_at DESC);
