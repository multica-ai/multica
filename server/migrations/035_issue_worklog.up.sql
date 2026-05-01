CREATE TABLE worklog (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    author_type TEXT NOT NULL CHECK (author_type IN ('member', 'agent')),
    author_id UUID NOT NULL,
    duration_minutes INT NOT NULL CHECK (duration_minutes > 0),
    description TEXT,
    type TEXT NOT NULL DEFAULT 'manual' CHECK (type IN ('manual', 'pomodoro')),
    logged_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE worklog_issue (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    worklog_id UUID NOT NULL REFERENCES worklog(id) ON DELETE CASCADE,
    issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (worklog_id, issue_id)
);

CREATE INDEX idx_worklog_workspace_logged_at ON worklog (workspace_id, logged_at DESC);
CREATE INDEX idx_worklog_issue_issue_workspace ON worklog_issue (issue_id, workspace_id, created_at DESC);