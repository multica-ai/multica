-- Revert: rename issue status 'needs_review' → 'in_review'.
-- 1. Drop the constraint so the UPDATE is allowed.
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;

-- 2. Revert existing rows.
UPDATE issue SET status = 'in_review' WHERE status = 'needs_review';

-- 3. Re-add the original constraint.
ALTER TABLE issue ADD CONSTRAINT issue_status_check
    CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled'));
