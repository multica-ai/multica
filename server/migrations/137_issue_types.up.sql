CREATE TABLE issue_types (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    icon TEXT NOT NULL DEFAULT 'circle',
    color TEXT NOT NULL DEFAULT '#6B7280',
    is_default BOOLEAN NOT NULL DEFAULT false,
    position INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, name)
);
CREATE INDEX idx_issue_types_workspace ON issue_types(workspace_id);

ALTER TABLE issue ADD COLUMN issue_type_id UUID REFERENCES issue_types(id) ON DELETE SET NULL;
CREATE INDEX idx_issue_type ON issue(issue_type_id);
