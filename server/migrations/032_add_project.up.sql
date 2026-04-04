-- Create project table
CREATE TABLE project (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    description     TEXT,
    status          TEXT NOT NULL DEFAULT 'backlog'
                    CHECK (status IN ('backlog', 'planned', 'in_progress', 'completed', 'cancelled')),
    icon            TEXT,
    color           TEXT,
    lead_type       TEXT CHECK (lead_type IN ('member', 'agent')),
    lead_id         UUID,
    start_date      DATE,
    target_date     DATE,
    sort_order      FLOAT8 NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_project_workspace ON project(workspace_id);
CREATE INDEX idx_project_workspace_status ON project(workspace_id, status);

-- Add project_id to issue
ALTER TABLE issue ADD COLUMN project_id UUID REFERENCES project(id) ON DELETE SET NULL;
CREATE INDEX idx_issue_project ON issue(workspace_id, project_id);
