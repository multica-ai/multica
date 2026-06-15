-- Sprint feature: sprint table, sprint_issue join, estimate field on issue

-- Add estimate field to issue
ALTER TABLE issue ADD COLUMN IF NOT EXISTS estimate INT;

-- Sprint table
CREATE TABLE sprint (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id   UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    goal         TEXT,
    start_date   TIMESTAMPTZ,
    end_date     TIMESTAMPTZ,
    state        TEXT NOT NULL DEFAULT 'planning'
                     CHECK (state IN ('planning', 'active', 'completed')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Only one active sprint per project at a time
CREATE UNIQUE INDEX sprint_one_active_per_project
    ON sprint (project_id) WHERE state = 'active';

-- sprint_id on issue (nullable — NULL = backlog)
ALTER TABLE issue ADD COLUMN IF NOT EXISTS sprint_id UUID REFERENCES sprint(id) ON DELETE SET NULL;

CREATE INDEX sprint_issue_sprint_id ON issue (sprint_id) WHERE sprint_id IS NOT NULL;
CREATE INDEX sprint_project_workspace ON sprint (workspace_id, project_id);
