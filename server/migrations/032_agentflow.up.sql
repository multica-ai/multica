-- Agentflow: scheduled/triggered agent tasks that run independently of issues.

-- Agentflows define reusable agent tasks with prompt templates.
CREATE TABLE agentflow (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT, -- prompt template, supports {{VARIABLE}} interpolation
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'paused', 'archived')),
    concurrency_policy TEXT NOT NULL DEFAULT 'skip_if_active'
        CHECK (concurrency_policy IN ('skip_if_active', 'coalesce', 'always_run')),
    variables JSONB NOT NULL DEFAULT '[]', -- variable definitions array
    created_by UUID NOT NULL REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agentflow_workspace ON agentflow(workspace_id)
    WHERE status != 'archived';

-- Triggers define when an agentflow fires (schedule, webhook, api).
CREATE TABLE agentflow_trigger (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agentflow_id UUID NOT NULL REFERENCES agentflow(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('schedule', 'webhook', 'api')),
    enabled BOOLEAN NOT NULL DEFAULT true,

    -- schedule fields
    cron_expression TEXT,
    timezone TEXT DEFAULT 'UTC',
    next_run_at TIMESTAMPTZ,

    -- webhook fields (reserved for phase 2)
    public_id TEXT UNIQUE,
    secret_hash TEXT,
    signing_mode TEXT CHECK (signing_mode IN ('bearer', 'hmac_sha256')),

    last_fired_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Efficient lookup for the scheduler: find schedule triggers that are due.
CREATE INDEX idx_agentflow_trigger_schedule_due
    ON agentflow_trigger(next_run_at)
    WHERE kind = 'schedule' AND enabled = true;

-- Runs track each execution of an agentflow.
CREATE TABLE agentflow_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agentflow_id UUID NOT NULL REFERENCES agentflow(id) ON DELETE CASCADE,
    trigger_id UUID REFERENCES agentflow_trigger(id) ON DELETE SET NULL,
    source_kind TEXT NOT NULL CHECK (source_kind IN ('schedule', 'webhook', 'api', 'manual')),
    status TEXT NOT NULL DEFAULT 'received'
        CHECK (status IN ('received', 'executing', 'completed', 'failed', 'skipped', 'coalesced')),
    linked_issue_id UUID REFERENCES issue(id) ON DELETE SET NULL, -- nullable: agent decides if issue needed
    payload JSONB, -- variable values + trigger payload snapshot
    agent_output TEXT, -- agent's execution summary
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    idempotency_key TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_agentflow_run_agentflow ON agentflow_run(agentflow_id, created_at DESC);

-- Extend agent_task_queue to support agentflow-triggered tasks.
-- issue_id becomes nullable (agentflow tasks may not have an issue).
ALTER TABLE agent_task_queue ALTER COLUMN issue_id DROP NOT NULL;

-- Link tasks to agentflow runs.
ALTER TABLE agent_task_queue ADD COLUMN agentflow_run_id UUID
    REFERENCES agentflow_run(id) ON DELETE SET NULL;
