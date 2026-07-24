-- Roll back the `archived` issue status.  Shrinking the enum while live rows
-- still carry status='archived' would violate the restored CHECK, so abort with
-- a clear error first: run scripts/sweep_archived_issues.sql to re-home those
-- rows, then re-apply this down migration.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM issue WHERE status = 'archived') THEN
        RAISE EXCEPTION 'cannot roll back migration 213: archived rows exist; run scripts/sweep_archived_issues.sql first';
    END IF;
END $$;

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
    )) NOT VALID;
