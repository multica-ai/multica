-- 1021_remove_in_review.up.sql
--
-- Drops the `in_review` issue status entirely. With the Phase 1 CR ↔ Resolving
-- loop (migration 1010 + state-machine redesign), `in_review` is dead weight:
-- it was a Phase 2 parking lot between Rosa's `<!-- resolution-note -->` and
-- Marcus's `<!-- pr-republished -->` markers — neither of which is wired.
--
-- Rows still parked in `in_review` are reset to `todo`: with the loop gone,
-- they need re-planning rather than re-entering the CR cycle mid-flight.

BEGIN;

-- (1) Reset any rows currently in in_review back to todo.
UPDATE issue
   SET status = 'todo',
       updated_at = now()
 WHERE status = 'in_review';

-- (2) Replace the CHECK constraint with one that excludes 'in_review'.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'issue_status_check'
          AND pg_get_constraintdef(oid) LIKE '%in_review%'
    ) THEN
        ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;
        ALTER TABLE issue ADD CONSTRAINT issue_status_check CHECK (
            status = ANY (ARRAY[
                'backlog','todo','in_progress','done','blocked','cancelled',
                'planning','ready_for_dev','code_review','fixing','testing','staged',
                'coderabbit','resolving'
            ])
        );
    END IF;
END $$;

COMMIT;
