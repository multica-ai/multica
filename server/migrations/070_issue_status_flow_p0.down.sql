-- Reverse PUL-13 P0. Restores the original whitelist, drops deployed_at, drops
-- issue_status_history. Will fail loudly if any issue rows currently hold one
-- of the new statuses (waiting/planned/developing/deployed) — that is the
-- desired behavior, the down should not silently coerce live data back.

BEGIN;

DROP INDEX IF EXISTS ix_issue_status_history_issue;
DROP TABLE IF EXISTS issue_status_history;

ALTER TABLE issue DROP COLUMN IF EXISTS deployed_at;

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;
ALTER TABLE issue ADD CONSTRAINT issue_status_check
    CHECK (status IN (
        'backlog',
        'todo',
        'in_progress',
        'in_review',
        'done',
        'blocked',
        'cancelled'
    ));

COMMIT;
