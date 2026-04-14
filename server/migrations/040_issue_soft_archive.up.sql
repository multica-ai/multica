ALTER TABLE issue
    ADD COLUMN IF NOT EXISTS archived_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS archived_by UUID REFERENCES "user"(id);

CREATE INDEX IF NOT EXISTS idx_issue_workspace_archived
    ON issue(workspace_id, archived_at);
