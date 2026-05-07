UPDATE issue SET status = 'done' WHERE status = 'archive';

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;

ALTER TABLE issue ADD CONSTRAINT issue_status_check
    CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled'));
