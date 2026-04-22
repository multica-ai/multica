CREATE TABLE structured_task_template (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    template_name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    goal TEXT NOT NULL DEFAULT '',
    audience JSONB NOT NULL DEFAULT '[]',
    output TEXT NOT NULL DEFAULT '',
    constraints JSONB NOT NULL DEFAULT '[]',
    style JSONB NOT NULL DEFAULT '[]',
    parameters JSONB NOT NULL DEFAULT '[]',
    scope TEXT NOT NULL DEFAULT 'personal',
    created_by UUID REFERENCES "user"(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE structured_task_history (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
    goal TEXT NOT NULL DEFAULT '',
    used_template_id UUID REFERENCES structured_task_template(id) ON DELETE SET NULL,
    clarity_status TEXT NOT NULL DEFAULT 'clear',
    spec JSONB NOT NULL DEFAULT '{}',
    created_by UUID REFERENCES "user"(id),
    executed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_structured_task_template_workspace ON structured_task_template(workspace_id, created_at DESC);
CREATE INDEX idx_structured_task_history_workspace ON structured_task_history(workspace_id, executed_at DESC);
