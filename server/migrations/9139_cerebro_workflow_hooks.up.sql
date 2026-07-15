CREATE TABLE IF NOT EXISTS cerebro_workflow_hook_policy (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    family_id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    policy_version INTEGER NOT NULL DEFAULT 1 CHECK (policy_version > 0),
    mode TEXT NOT NULL DEFAULT 'dry_run'
        CHECK (mode IN ('off', 'dry_run', 'enforce', 'managed')),
    fail_mode TEXT NOT NULL DEFAULT 'open'
        CHECK (fail_mode IN ('open', 'closed', 'warn')),
    event_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    conditions JSONB NOT NULL DEFAULT '[]'::jsonb,
    baseline_at TIMESTAMPTZ,
    published_at TIMESTAMPTZ,
    created_by_id UUID NOT NULL,
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    published_by_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, family_id, policy_version)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_workflow_hook_policy_workspace_mode
    ON cerebro_workflow_hook_policy(workspace_id, mode, updated_at DESC);

CREATE TABLE IF NOT EXISTS cerebro_workflow_hook_binding (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id UUID NOT NULL REFERENCES cerebro_workflow_hook_policy(id) ON DELETE CASCADE,
    scope_kind TEXT NOT NULL
        CHECK (scope_kind IN ('workspace', 'project', 'workflow', 'agent', 'model', 'issue', 'session')),
    scope_id TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (policy_id, scope_kind, scope_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_workflow_hook_binding_scope
    ON cerebro_workflow_hook_binding(scope_kind, scope_id, priority DESC);

CREATE TABLE IF NOT EXISTS cerebro_workflow_hook_handler (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    policy_id UUID NOT NULL REFERENCES cerebro_workflow_hook_policy(id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0,
    decision TEXT NOT NULL CHECK (decision IN ('allow', 'block', 'modify', 'require')),
    requirement TEXT NOT NULL DEFAULT '',
    modifications JSONB NOT NULL DEFAULT '{}'::jsonb,
    actions JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (policy_id, position)
);

CREATE TABLE IF NOT EXISTS cerebro_workflow_hook_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    policy_id UUID NOT NULL REFERENCES cerebro_workflow_hook_policy(id) ON DELETE CASCADE,
    policy_version INTEGER NOT NULL,
    event_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    source_scope JSONB NOT NULL DEFAULT '{}'::jsonb,
    input_event JSONB NOT NULL DEFAULT '{}'::jsonb,
    matched_conditions JSONB NOT NULL DEFAULT '[]'::jsonb,
    decision TEXT NOT NULL CHECK (decision IN ('allow', 'block', 'modify', 'require')),
    would_decision TEXT,
    fail_mode TEXT NOT NULL CHECK (fail_mode IN ('open', 'closed', 'warn')),
    remediation TEXT NOT NULL DEFAULT '',
    latency_ms INTEGER NOT NULL DEFAULT 0,
    timed_out BOOLEAN NOT NULL DEFAULT FALSE,
    idempotency_key TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_workflow_hook_run_history
    ON cerebro_workflow_hook_run(workspace_id, created_at DESC);

CREATE TABLE IF NOT EXISTS cerebro_workflow_hook_action_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    hook_run_id UUID NOT NULL REFERENCES cerebro_workflow_hook_run(id) ON DELETE CASCADE,
    handler_id UUID REFERENCES cerebro_workflow_hook_handler(id) ON DELETE SET NULL,
    action_index INTEGER NOT NULL DEFAULT 0,
    action_type TEXT NOT NULL,
    action_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL CHECK (status IN ('would_run', 'running', 'success', 'failed', 'denied')),
    result JSONB NOT NULL DEFAULT '{}'::jsonb,
    error TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (hook_run_id, action_index)
);
