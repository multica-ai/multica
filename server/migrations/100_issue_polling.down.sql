DROP INDEX IF EXISTS idx_issue_polling_due;

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_poll_interval_minutes_check;
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;
ALTER TABLE issue ADD CONSTRAINT issue_status_check
    CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled'));

ALTER TABLE issue
    DROP COLUMN IF EXISTS poll_run_count,
    DROP COLUMN IF EXISTS poll_last_run,
    DROP COLUMN IF EXISTS poll_next_run,
    DROP COLUMN IF EXISTS poll_interval_minutes,
    DROP COLUMN IF EXISTS poll_start_at;
