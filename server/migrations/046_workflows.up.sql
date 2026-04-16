-- Workflow definitions
CREATE TABLE workflow (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    steps         JSONB NOT NULL DEFAULT '[]',
    created_by    UUID NOT NULL REFERENCES "user"(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    archived_at   TIMESTAMPTZ
);

CREATE INDEX idx_workflow_workspace ON workflow(workspace_id) WHERE archived_at IS NULL;

-- Workflow execution instances
CREATE TABLE workflow_run (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id        UUID NOT NULL REFERENCES workflow(id) ON DELETE CASCADE,
    workspace_id       UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    trigger_type       TEXT NOT NULL CHECK (trigger_type IN ('schedule', 'manual')),
    trigger_id         UUID,
    status             TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'paused', 'completed', 'failed', 'cancelled')),
    current_step_index INT NOT NULL DEFAULT 0,
    issue_id           UUID REFERENCES issue(id) ON DELETE SET NULL,
    context            JSONB NOT NULL DEFAULT '{}',
    started_at         TIMESTAMPTZ,
    completed_at       TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_workflow_run_workflow ON workflow_run(workflow_id);
CREATE INDEX idx_workflow_run_workspace ON workflow_run(workspace_id);
CREATE INDEX idx_workflow_run_status ON workflow_run(workspace_id, status) WHERE status IN ('pending', 'running', 'paused');

-- Individual step execution records
CREATE TABLE workflow_step_run (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_run_id  UUID NOT NULL REFERENCES workflow_run(id) ON DELETE CASCADE,
    step_index       INT NOT NULL,
    step_type        TEXT NOT NULL CHECK (step_type IN ('agent', 'approval')),
    status           TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'completed', 'failed', 'skipped', 'rejected')),
    agent_task_id    UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    reviewer_id      UUID REFERENCES "user"(id),
    decision         TEXT CHECK (decision IN ('approved', 'rejected', 'stopped')),
    decision_comment TEXT,
    started_at       TIMESTAMPTZ,
    completed_at     TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_workflow_step_run_run ON workflow_step_run(workflow_run_id);
CREATE INDEX idx_workflow_step_run_task ON workflow_step_run(agent_task_id) WHERE agent_task_id IS NOT NULL;
