-- Project table
CREATE TABLE project (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT,
    icon TEXT,
    status TEXT NOT NULL DEFAULT 'planned'
        CHECK (status IN ('planned', 'in_progress', 'paused', 'completed', 'cancelled')),
    lead_type TEXT CHECK (lead_type IN ('member', 'agent')),
    lead_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_project_workspace ON project(workspace_id);

-- Add project_id to issue
ALTER TABLE issue ADD COLUMN project_id UUID REFERENCES project(id) ON DELETE SET NULL;
CREATE INDEX idx_issue_project ON issue(project_id);
