-- Reverse of 072_cascade_event_driven.up.sql.
-- Order: drop dependents first (cascade_pending_event references
-- cascade_retrigger.event_id; both reference issue). Indexes drop with their
-- tables; the two indexes on issue must be dropped explicitly before column
-- removal.

BEGIN;

DROP TABLE IF EXISTS cascade_pending_event;
DROP TABLE IF EXISTS cascade_retrigger;

DROP INDEX IF EXISTS idx_issue_cascade_started;
DROP INDEX IF EXISTS idx_issue_cascade_active;

ALTER TABLE issue
    DROP COLUMN IF EXISTS cascade_progress,
    DROP COLUMN IF EXISTS cascade_last_event_at,
    DROP COLUMN IF EXISTS cascade_started_at,
    DROP COLUMN IF EXISTS cascade_state;

COMMIT;
