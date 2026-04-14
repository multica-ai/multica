DROP INDEX IF EXISTS idx_issue_workspace_archived;

ALTER TABLE issue
    DROP COLUMN IF EXISTS archived_by,
    DROP COLUMN IF EXISTS archived_at;
