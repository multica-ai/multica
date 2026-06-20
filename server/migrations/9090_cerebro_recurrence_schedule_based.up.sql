-- FIR-1639: schedule-based recurrence for recurring issues.
--
-- Until now recurrence was completion-gated: the sweeper waited for the source
-- issue to edge-transition into its trigger status before spawning the next
-- occurrence. Schedule-based recurrence instead spawns a new occurrence at each
-- point in the schedule, independently of whether the previous one is done.
--
-- That requires a stored "clock": next_run_at holds when a schedule rule fires
-- next. For completion rules it stays NULL and nothing changes.
--
-- New columns on cerebro_issue_recurrence:
--   trigger_type   — 'completion' (default, today's behaviour) | 'schedule'.
--   stale_handling — when a schedule fires and the previous occurrence is still
--                    open: 'leave_open' (default) | 'auto_close'.
--   stale_status   — target status the previous occurrence is moved to when
--                    stale_handling='auto_close' (e.g. 'cancelled'). Validated
--                    in Go like trigger_status/new_status, so no CHECK here.
--   date_format    — format used to substitute the {{date}} title placeholder
--                    for schedule occurrences: 'iso' (YYYY-MM-DD) | 'dk'
--                    (DD-MM-YYYY).
--   next_run_at    — TIMESTAMPTZ; when a schedule rule fires next. NULL for
--                    completion rules.
--
-- Idempotent (ADD COLUMN IF NOT EXISTS) so the migration test's re-run pass is
-- a no-op. Mirrors the additive style of migration 9089.

ALTER TABLE cerebro_issue_recurrence
    ADD COLUMN IF NOT EXISTS trigger_type   TEXT NOT NULL DEFAULT 'completion',
    ADD COLUMN IF NOT EXISTS stale_handling TEXT NOT NULL DEFAULT 'leave_open',
    ADD COLUMN IF NOT EXISTS stale_status   TEXT NOT NULL DEFAULT 'cancelled',
    ADD COLUMN IF NOT EXISTS date_format    TEXT NOT NULL DEFAULT 'iso',
    ADD COLUMN IF NOT EXISTS next_run_at    TIMESTAMPTZ;

-- Widen with named CHECK constraints (drop-if-exists first so re-running is a
-- no-op and a future rollback can find them by name).
ALTER TABLE cerebro_issue_recurrence
    DROP CONSTRAINT IF EXISTS cerebro_issue_recurrence_trigger_type_check;
ALTER TABLE cerebro_issue_recurrence
    ADD CONSTRAINT cerebro_issue_recurrence_trigger_type_check
    CHECK (trigger_type IN ('completion', 'schedule'));

ALTER TABLE cerebro_issue_recurrence
    DROP CONSTRAINT IF EXISTS cerebro_issue_recurrence_stale_handling_check;
ALTER TABLE cerebro_issue_recurrence
    ADD CONSTRAINT cerebro_issue_recurrence_stale_handling_check
    CHECK (stale_handling IN ('leave_open', 'auto_close'));

ALTER TABLE cerebro_issue_recurrence
    DROP CONSTRAINT IF EXISTS cerebro_issue_recurrence_date_format_check;
ALTER TABLE cerebro_issue_recurrence
    ADD CONSTRAINT cerebro_issue_recurrence_date_format_check
    CHECK (date_format IN ('iso', 'dk'));

-- Sweeper hot path for schedule rules: find enabled schedule rules whose clock
-- is due. Partial index keeps it cheap alongside the existing enabled index.
CREATE INDEX IF NOT EXISTS idx_cerebro_issue_recurrence_next_run
    ON cerebro_issue_recurrence (next_run_at)
    WHERE trigger_type = 'schedule' AND enabled = TRUE;
