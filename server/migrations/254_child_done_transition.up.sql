CREATE TABLE child_done_transition (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    group_id UUID NOT NULL,
    child_issue_id UUID NOT NULL,
    parent_issue_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    terminal_status TEXT NOT NULL
        CHECK (terminal_status IN ('done', 'blocked', 'cancelled')),
    stage INTEGER,
    transition_at TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'processed')),
    group_ready BOOLEAN NOT NULL DEFAULT TRUE,
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    lease_token UUID,
    lease_expires_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0
        CHECK (attempts >= 0),
    error TEXT
);
