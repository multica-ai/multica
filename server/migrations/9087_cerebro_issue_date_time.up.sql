-- Fork-specific: holds an OPTIONAL time-of-day for an issue's start_date and
-- due_date. The dates themselves stay pure calendar days on the issue table
-- (timezone-agnostic, migration 112); this companion table only adds the
-- wall-clock time the user opted into. A NULL time means "no time set" and the
-- date keeps behaving exactly as before (fires at the start of the day).
--
-- The interpretation timezone is NOT stored here: the date-reminder sweeper
-- resolves it from the assignee (member reminders) or the issue's member
-- creator (agent auto-start), reusing cerebro_safe_timezone().
CREATE TABLE IF NOT EXISTS cerebro_issue_date_time (
    issue_id   UUID PRIMARY KEY REFERENCES issue(id) ON DELETE CASCADE,
    start_time TIME,
    due_time   TIME,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
