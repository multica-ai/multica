-- All writers must already emit lifecycle categories. NOT VALID avoids a
-- table scan while holding ACCESS EXCLUSIVE; 493 validates after commit.
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '10s';

ALTER TABLE issue_status
    DROP CONSTRAINT IF EXISTS issue_status_category_check,
    DROP CONSTRAINT IF EXISTS issue_status_system_is_canonical,
    ADD CONSTRAINT issue_status_category_check CHECK (
        category IN ('unstarted', 'started', 'done', 'closed')
    ) NOT VALID,
    ADD CONSTRAINT issue_status_system_is_canonical CHECK (
        NOT is_system OR (key, category) IN (
            ('backlog', 'unstarted'), ('todo', 'unstarted'),
            ('in_progress', 'started'), ('in_review', 'started'),
            ('blocked', 'started'), ('done', 'done'), ('cancelled', 'closed')
        )
    ) NOT VALID;
