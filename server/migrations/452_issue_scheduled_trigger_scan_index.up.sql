-- Serves the scheduler's per-tick scope query: the pending schedules due by
-- now, ordered by run_at.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_issue_scheduled_trigger_status_run_at
    ON issue_scheduled_trigger (status, run_at);
