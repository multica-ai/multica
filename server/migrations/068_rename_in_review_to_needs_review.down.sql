-- Revert: rename issue status 'needs_review' → 'in_review'.
UPDATE issue SET status = 'in_review' WHERE status = 'needs_review';

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;
ALTER TABLE issue ADD CONSTRAINT issue_status_check
    CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled'));
