CREATE TABLE issue_time_entry (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('member', 'agent')),
    actor_id UUID NOT NULL,
    task_id UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    source TEXT NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'agent_task')),
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    stopped_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (stopped_at IS NULL OR stopped_at >= started_at)
);

CREATE INDEX idx_issue_time_entry_issue ON issue_time_entry(issue_id, started_at DESC);
CREATE INDEX idx_issue_time_entry_workspace_actor ON issue_time_entry(workspace_id, actor_type, actor_id);
CREATE UNIQUE INDEX idx_issue_time_entry_active_actor
    ON issue_time_entry(workspace_id, actor_type, actor_id)
    WHERE stopped_at IS NULL;
CREATE UNIQUE INDEX idx_issue_time_entry_active_task
    ON issue_time_entry(task_id)
    WHERE task_id IS NOT NULL AND stopped_at IS NULL;
