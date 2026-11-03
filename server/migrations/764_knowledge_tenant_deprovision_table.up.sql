-- A workspace's WeKnora tenant, queued for removal by the workspace delete
-- (plan 10.14). The delete transaction writes the row, so a confirmed deletion
-- always leaves one; the tenant is removed only once hold_until has passed. The
-- row carries workspace_id as a plain value with no foreign key, because the
-- workspace is gone by the time the job reads it.
CREATE TABLE knowledge_tenant_deprovision (
    workspace_id UUID NOT NULL,
    weknora_tenant_id TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('held', 'running', 'done')),
    hold_until TIMESTAMPTZ NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    claimed_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);
