-- Epic: logical grouping container for related issues
CREATE TABLE epic (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT,
    color TEXT NOT NULL DEFAULT '#6366f1',
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_epic_workspace ON epic(workspace_id);

-- Sprint: time-boxed iteration
CREATE TABLE sprint (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    goal TEXT,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    status TEXT NOT NULL DEFAULT 'planned'
        CHECK (status IN ('planned', 'active', 'completed', 'cancelled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sprint_workspace ON sprint(workspace_id);
CREATE INDEX idx_sprint_status ON sprint(workspace_id, status);

-- Issue table: add FK columns
ALTER TABLE issue ADD COLUMN epic_id UUID REFERENCES epic(id) ON DELETE SET NULL;
ALTER TABLE issue ADD COLUMN sprint_id UUID REFERENCES sprint(id) ON DELETE SET NULL;

CREATE INDEX idx_issue_epic ON issue(epic_id);
CREATE INDEX idx_issue_sprint ON issue(sprint_id);
