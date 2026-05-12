-- 1021_remove_in_review.down.sql
--
-- Re-add 'in_review' to the CHECK constraint. No row backfill — once code is
-- rolled back the value is unreachable from app code anyway.

BEGIN;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'issue_status_check'
          AND pg_get_constraintdef(oid) LIKE '%in_review%'
    ) THEN
        ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;
        ALTER TABLE issue ADD CONSTRAINT issue_status_check CHECK (
            status = ANY (ARRAY[
                'backlog','todo','in_progress','in_review','done','blocked','cancelled',
                'planning','ready_for_dev','code_review','fixing','testing','staged',
                'coderabbit','resolving'
            ])
        );
    END IF;
END $$;

COMMIT;
