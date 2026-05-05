ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;
ALTER TABLE issue ADD CONSTRAINT issue_status_check
    CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled'));

DROP TABLE IF EXISTS issue_status_transition;
DROP TABLE IF EXISTS issue_status_def;
DROP TABLE IF EXISTS issue_workflow;
