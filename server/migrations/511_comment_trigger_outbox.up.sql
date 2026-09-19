CREATE TABLE comment_trigger_outbox (
    comment_id UUID PRIMARY KEY REFERENCES comment(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member', 'agent')),
    actor_id UUID NOT NULL,
    originator_user_id UUID,
    suppress_agent_ids UUID[] NOT NULL DEFAULT '{}',
    state TEXT NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending', 'processing', 'done', 'dead')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    lease_owner TEXT,
    lease_expires_at TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ
);

CREATE INDEX idx_comment_trigger_outbox_pending
ON comment_trigger_outbox (COALESCE(next_attempt_at, created_at), created_at, comment_id)
WHERE state IN ('pending', 'processing');
