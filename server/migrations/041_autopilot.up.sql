CREATE TABLE autopilot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'paused')),
    mode TEXT NOT NULL DEFAULT 'create_issue'
        CHECK (mode IN ('create_issue')),
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    project_id UUID REFERENCES project(id) ON DELETE SET NULL,
    priority TEXT NOT NULL DEFAULT 'none'
        CHECK (priority IN ('urgent', 'high', 'medium', 'low', 'none')),
    issue_title_template TEXT NOT NULL DEFAULT '',
    created_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);

CREATE TABLE autopilot_trigger (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    autopilot_id UUID NOT NULL REFERENCES autopilot(id) ON DELETE CASCADE,
    type TEXT NOT NULL CHECK (type IN ('schedule')),
    label TEXT,
    cron TEXT,
    timezone TEXT NOT NULL DEFAULT 'UTC',
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'paused')),
    next_run_at TIMESTAMPTZ,
    last_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE autopilot_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    autopilot_id UUID NOT NULL REFERENCES autopilot(id) ON DELETE CASCADE,
    trigger_id UUID REFERENCES autopilot_trigger(id) ON DELETE SET NULL,
    source TEXT NOT NULL CHECK (source IN ('manual', 'schedule')),
    status TEXT NOT NULL DEFAULT 'queued'
        CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'cancelled', 'skipped')),
    scheduled_for TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
    created_task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    error TEXT,
    idempotency_key TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_autopilot_workspace ON autopilot(workspace_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_autopilot_agent ON autopilot(agent_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_autopilot_trigger_autopilot ON autopilot_trigger(autopilot_id);
CREATE INDEX idx_autopilot_run_autopilot ON autopilot_run(autopilot_id, created_at DESC);
CREATE INDEX idx_autopilot_run_workspace ON autopilot_run(workspace_id, created_at DESC);
CREATE UNIQUE INDEX idx_autopilot_run_idempotency
    ON autopilot_run(autopilot_id, trigger_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
