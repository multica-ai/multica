-- Rename issue status 'in_review' → 'needs_review'.
-- 1. Drop the existing constraint so the UPDATE is allowed.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;

-- 2. Update existing rows.
UPDATE issue SET status = 'needs_review' WHERE status = 'in_review';

-- 3. Add the new constraint.
ALTER TABLE issue ADD CONSTRAINT issue_status_check
    CHECK (status IN ('backlog', 'todo', 'in_progress', 'needs_review', 'done', 'blocked', 'cancelled'));
