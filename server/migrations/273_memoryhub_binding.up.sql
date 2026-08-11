-- MemoryHub binding row: workspace/project scope, subject, sync status,
-- optimistic version, idempotency key, and the four remote-reference columns
-- that serialize as one remote_ref object. No inline PK/UNIQUE/INDEX/FK by
-- repository policy; the PK and uniqueness arrive in dedicated index +
-- constraint migrations.
CREATE TABLE memoryhub_binding (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    scope_kind TEXT NOT NULL CHECK (scope_kind IN ('workspace', 'project')),
    scope_id UUID,
    subject_type TEXT NOT NULL CHECK (subject_type IN ('project', 'agent', 'issue')),
    subject_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'unbound' CHECK (status IN ('unbound', 'binding', 'bound', 'sync_failed', 'compensating', 'blocked')),
    version INTEGER NOT NULL DEFAULT 1,
    idempotency_key TEXT NOT NULL,
    remote_team_id TEXT,
    remote_agent_id TEXT,
    remote_task_id TEXT,
    remote_name TEXT,
    evidence_ref UUID,
    next_wakeup TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ck_mh_scope CHECK (
        (scope_kind = 'workspace' AND scope_id IS NULL)
        OR (scope_kind = 'project' AND scope_id IS NOT NULL)
    )
);
