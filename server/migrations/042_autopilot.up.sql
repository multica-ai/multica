-- Autopilot: scheduled/triggered automations that assign recurring work to AI Agents.

CREATE TABLE IF NOT EXISTS autopilot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id UUID REFERENCES project(id) ON DELETE SET NULL,
    title TEXT NOT NULL,
    description TEXT,
    assignee_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    priority TEXT NOT NULL DEFAULT 'medium'
        CHECK (priority IN ('urgent', 'high', 'medium', 'low', 'none')),
    status TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'paused', 'archived')),
    execution_mode TEXT NOT NULL DEFAULT 'create_issue'
        CHECK (execution_mode IN ('create_issue', 'run_only')),
    issue_title_template TEXT,
    concurrency_policy TEXT NOT NULL DEFAULT 'skip'
        CHECK (concurrency_policy IN ('skip', 'queue', 'replace')),
    created_by_type TEXT NOT NULL CHECK (created_by_type IN ('member', 'agent')),
    created_by_id UUID NOT NULL,
    last_run_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_autopilot_workspace ON autopilot(workspace_id);
CREATE INDEX IF NOT EXISTS idx_autopilot_assignee ON autopilot(assignee_id);

-- Trigger: how an autopilot gets kicked off (schedule, webhook, or API).
CREATE TABLE IF NOT EXISTS autopilot_trigger (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    autopilot_id UUID NOT NULL REFERENCES autopilot(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK (kind IN ('schedule', 'webhook', 'api')),
    enabled BOOLEAN NOT NULL DEFAULT true,
    cron_expression TEXT,
    timezone TEXT DEFAULT 'UTC',
    next_run_at TIMESTAMPTZ,
    webhook_token TEXT,
    label TEXT,
    last_fired_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_autopilot_trigger_autopilot ON autopilot_trigger(autopilot_id);
CREATE INDEX IF NOT EXISTS idx_autopilot_trigger_next_run ON autopilot_trigger(next_run_at)
    WHERE enabled = true AND kind = 'schedule';

-- Run: one execution of an autopilot.
CREATE TABLE IF NOT EXISTS autopilot_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    autopilot_id UUID NOT NULL REFERENCES autopilot(id) ON DELETE CASCADE,
    trigger_id UUID REFERENCES autopilot_trigger(id) ON DELETE SET NULL,
    source TEXT NOT NULL CHECK (source IN ('schedule', 'manual', 'webhook', 'api')),
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'issue_created', 'running', 'skipped', 'completed', 'failed')),
    issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
    task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    triggered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    failure_reason TEXT,
    trigger_payload JSONB,
    result JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_autopilot_run_autopilot ON autopilot_run(autopilot_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_autopilot_run_status ON autopilot_run(autopilot_id, status)
    WHERE status IN ('pending', 'issue_created', 'running');

-- Link agent tasks to autopilot runs (same pattern as chat_session_id from migration 033).
ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS autopilot_run_id UUID REFERENCES autopilot_run(id) ON DELETE SET NULL;

-- Track which issues were created by an autopilot so they can be filtered in lists.
ALTER TABLE issue ADD COLUMN IF NOT EXISTS origin_type TEXT CHECK (origin_type IN ('autopilot'));
ALTER TABLE issue ADD COLUMN IF NOT EXISTS origin_id UUID;
CREATE INDEX IF NOT EXISTS idx_issue_origin ON issue(origin_type, origin_id) WHERE origin_type IS NOT NULL;
