-- We do not undo the backfill data to prevent data loss.
-- If they want to rollback, the data just stays in issue_assignees.
-- But since we haven't dropped the assignee_id column yet, nothing is lost.
SELECT 1;
