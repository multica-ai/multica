ALTER TABLE issue
    DROP CONSTRAINT IF EXISTS issue_status_check,
    ADD CONSTRAINT issue_status_check
        CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'review', 'done', 'blocked', 'cancelled'));
