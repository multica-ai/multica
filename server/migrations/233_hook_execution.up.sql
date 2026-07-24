-- Matcher decision and executor trace. The revision is pinned at creation.
CREATE TABLE hook_execution (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    hook_id UUID NOT NULL,
    hook_revision_id UUID NOT NULL,
    event_id UUID NOT NULL,
    correlation_id UUID NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'skipped')),
    skip_reason TEXT,
    match_snapshot JSONB,
    condition_snapshot JSONB,
    current_action_index INT NOT NULL DEFAULT 0,
    attempts INT NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ,
    lease_token UUID,
    lease_expires_at TIMESTAMPTZ,
    error_code TEXT,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ
);

COMMENT ON TABLE hook_execution IS 'Durable matcher/executor trace with revision pinning. No foreign keys; application validates all associations.';
