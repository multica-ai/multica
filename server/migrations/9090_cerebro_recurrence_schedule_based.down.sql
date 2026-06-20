-- FIR-1639: reverse of the schedule-based recurrence columns.

DROP INDEX IF EXISTS idx_cerebro_issue_recurrence_next_run;

ALTER TABLE cerebro_issue_recurrence
    DROP CONSTRAINT IF EXISTS cerebro_issue_recurrence_trigger_type_check,
    DROP CONSTRAINT IF EXISTS cerebro_issue_recurrence_stale_handling_check,
    DROP CONSTRAINT IF EXISTS cerebro_issue_recurrence_date_format_check;

ALTER TABLE cerebro_issue_recurrence
    DROP COLUMN IF EXISTS trigger_type,
    DROP COLUMN IF EXISTS stale_handling,
    DROP COLUMN IF EXISTS stale_status,
    DROP COLUMN IF EXISTS date_format,
    DROP COLUMN IF EXISTS next_run_at;
