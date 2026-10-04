-- One pending schedule per issue: a second POST /schedule while one is
-- already pending must fail with a clear conflict instead of silently
-- creating a second row the scheduler would fire twice.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_issue_scheduled_trigger_pending_issue
    ON issue_scheduled_trigger (issue_id)
    WHERE status = 'pending';
