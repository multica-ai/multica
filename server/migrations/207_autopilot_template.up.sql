-- Workspace-owned reusable definitions. A template deliberately has no
-- Space, Project, assignee, subscriber, or credential: those are chosen when
-- creating each concrete Space-owned Autopilot instance.
CREATE TABLE autopilot_template (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    execution_mode TEXT NOT NULL DEFAULT 'create_issue'
        CHECK (execution_mode IN ('create_issue', 'run_only')),
    issue_title_template TEXT,
    trigger_kind TEXT NOT NULL DEFAULT 'schedule'
        CHECK (trigger_kind IN ('schedule', 'webhook')),
    cron_expression TEXT,
    timezone TEXT,
    created_by UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, name),
    CHECK (
      (trigger_kind = 'schedule' AND cron_expression IS NOT NULL AND timezone IS NOT NULL)
      OR (trigger_kind = 'webhook' AND cron_expression IS NULL AND timezone IS NULL)
    )
);

CREATE INDEX idx_autopilot_template_workspace
    ON autopilot_template(workspace_id, created_at DESC);
