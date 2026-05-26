ALTER TABLE issue
    ADD COLUMN IF NOT EXISTS poll_start_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS poll_interval_minutes INT,
    ADD COLUMN IF NOT EXISTS poll_next_run TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS poll_last_run TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS poll_run_count INT NOT NULL DEFAULT 0;

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;
ALTER TABLE issue ADD CONSTRAINT issue_status_check
    CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled', 'polling'));

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_poll_interval_minutes_check;
ALTER TABLE issue ADD CONSTRAINT issue_poll_interval_minutes_check
    CHECK (poll_interval_minutes IS NULL OR poll_interval_minutes > 0);

CREATE INDEX IF NOT EXISTS idx_issue_polling_due
    ON issue (poll_next_run, created_at)
    WHERE status = 'polling' AND poll_next_run IS NOT NULL;
