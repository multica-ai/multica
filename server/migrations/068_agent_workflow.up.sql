-- agent_workflow + agent_workflow_run.
--
-- Sibling concept to a future team_workflow: an agent_workflow describes
-- a sequence of steps owned by a single agent. Steps may be 'agent'-actor
-- (executed by the runtime via a tool / skill / instructions) or
-- 'human'-actor (gated on an external approval signal — usually an
-- `issue:resolved` event from an approval-kind issue).
--
-- definition JSONB shape (validated by the service layer, not the schema):
--
--   {
--     "steps": [
--       { "id": "ingest",  "actor": "agent", "tool": "...",  "next": "compute" },
--       { "id": "compute", "actor": "agent", "skill": "...", "next": "draft"   },
--       { "id": "review",  "actor": "human", "gate": "issue_approval",
--         "next_on_approve": "file", "next_on_reject": "draft" },
--       { "id": "file",    "actor": "agent", "tool": "...",  "next": "archive" },
--       { "id": "archive", "actor": "agent", "tool": "..." }
--     ]
--   }
--
-- Snapshot policy: an in-flight run should not be disturbed when the
-- workflow definition is edited. The recommended pattern is to copy the
-- definition JSONB onto agent_workflow_run.state.definition at run-create
-- time. This is enforced by the service layer, not by the schema.
--
-- Correlation columns (last_task_id / last_issue_id) let the workflow
-- engine route inbound `task:completed` and `issue:resolved` events back
-- to the run that's parked on them, without requiring a payload-schema
-- change in lockstep with the engine.
CREATE TABLE IF NOT EXISTS agent_workflow (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agent(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    definition JSONB NOT NULL DEFAULT '{}',
    archived_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, agent_id, name)
);

CREATE INDEX IF NOT EXISTS idx_agent_workflow_workspace
    ON agent_workflow(workspace_id);
CREATE INDEX IF NOT EXISTS idx_agent_workflow_agent
    ON agent_workflow(agent_id);

CREATE TABLE IF NOT EXISTS agent_workflow_run (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL REFERENCES agent_workflow(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    started_by UUID NULL REFERENCES "user"(id) ON DELETE SET NULL,
    status TEXT NOT NULL DEFAULT 'running'
        CHECK (status IN ('running', 'awaiting_approval', 'done', 'failed')),
    current_step TEXT NOT NULL DEFAULT '',
    state JSONB NOT NULL DEFAULT '{}',
    -- last_task_id / last_issue_id are the engine's correlation handles.
    -- Exactly one is non-NULL while the run is parked on a task or an
    -- approval issue; both are NULL while the run is between steps.
    last_task_id UUID NULL,
    last_issue_id UUID NULL REFERENCES issue(id) ON DELETE SET NULL,
    error TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_workflow_run_workflow
    ON agent_workflow_run(workflow_id);
CREATE INDEX IF NOT EXISTS idx_agent_workflow_run_workspace
    ON agent_workflow_run(workspace_id);
CREATE INDEX IF NOT EXISTS idx_agent_workflow_run_status
    ON agent_workflow_run(workspace_id, status);
CREATE INDEX IF NOT EXISTS idx_agent_workflow_run_task
    ON agent_workflow_run(last_task_id) WHERE last_task_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_agent_workflow_run_issue
    ON agent_workflow_run(last_issue_id) WHERE last_issue_id IS NOT NULL;
