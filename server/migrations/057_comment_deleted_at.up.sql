ALTER TABLE comment ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_comment_active_issue_created
    ON comment(issue_id, workspace_id, created_at)
    WHERE deleted_at IS NULL;
