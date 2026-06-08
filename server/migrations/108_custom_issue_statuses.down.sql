-- Restore the hardcoded CHECK constraint on issue.status.
ALTER TABLE issue ADD CONSTRAINT issue_status_check
    CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled'));

-- Drop custom statuses table.
DROP TABLE IF EXISTS workspace_issue_status;
