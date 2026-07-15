-- FIR-3308: first-class eval catalog and workflow gates.
-- Cerebro owns catalog metadata, bindings, and run state. Versioned cases,
-- graders, and runner assets remain in the referenced source repository.

CREATE TABLE IF NOT EXISTS cerebro_eval (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    eval_key TEXT NOT NULL,
    version TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'active', 'paused', 'retired')),
    owner JSONB NOT NULL DEFAULT '{}'::jsonb,
    objective TEXT NOT NULL,
    target JSONB NOT NULL,
    datasets JSONB NOT NULL DEFAULT '[]'::jsonb,
    graders JSONB NOT NULL DEFAULT '[]'::jsonb,
    thresholds JSONB NOT NULL DEFAULT '[]'::jsonb,
    runner JSONB NOT NULL DEFAULT '{}'::jsonb,
    source JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by_id UUID NOT NULL,
    created_by_type TEXT NOT NULL DEFAULT 'member'
        CHECK (created_by_type IN ('member', 'agent')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT cerebro_eval_key_format CHECK (eval_key ~ '^[a-z0-9]+(?:-[a-z0-9]+)*$'),
    CONSTRAINT cerebro_eval_version_format CHECK (version ~ '^[0-9]+\.[0-9]+\.[0-9]+$'),
    UNIQUE (workspace_id, eval_key, version)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_eval_workspace_status
    ON cerebro_eval(workspace_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_cerebro_eval_workspace_key
    ON cerebro_eval(workspace_id, eval_key, updated_at DESC);

CREATE TABLE IF NOT EXISTS cerebro_eval_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    eval_id UUID NOT NULL REFERENCES cerebro_eval(id) ON DELETE CASCADE,
    eval_version TEXT NOT NULL,
    target_version TEXT NOT NULL DEFAULT '',
    workflow_id UUID REFERENCES cerebro_workflow(id) ON DELETE SET NULL,
    issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
    status TEXT NOT NULL
        CHECK (status IN ('queued', 'running', 'passed', 'failed', 'error')),
    results JSONB NOT NULL DEFAULT '{}'::jsonb,
    evidence_artifact_id UUID,
    cost_cents BIGINT NOT NULL DEFAULT 0 CHECK (cost_cents >= 0),
    latency_ms BIGINT NOT NULL DEFAULT 0 CHECK (latency_ms >= 0),
    created_by_id UUID NOT NULL,
    created_by_type TEXT NOT NULL DEFAULT 'member'
        CHECK (created_by_type IN ('member', 'agent')),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_cerebro_eval_run_eval_created
    ON cerebro_eval_run(eval_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_cerebro_eval_run_gate_lookup
    ON cerebro_eval_run(workspace_id, workflow_id, issue_id, eval_id, created_at DESC);

CREATE TABLE IF NOT EXISTS cerebro_workflow_eval_binding (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    workflow_id UUID NOT NULL REFERENCES cerebro_workflow(id) ON DELETE CASCADE,
    eval_id UUID NOT NULL REFERENCES cerebro_eval(id) ON DELETE CASCADE,
    phase TEXT NOT NULL DEFAULT 'delivery'
        CHECK (phase IN ('plan', 'delivery', 'monitor')),
    blocking BOOLEAN NOT NULL DEFAULT TRUE,
    created_by_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workflow_id, eval_id, phase)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_workflow_eval_binding_gate
    ON cerebro_workflow_eval_binding(workspace_id, workflow_id, phase, blocking);
