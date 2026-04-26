-- 1002_remove_checkpoint.up.sql
--
-- Removes the `checkpoint` issue status per the BMAD workflow spec.
--
-- Rationale: the BMAD card-flow contract has 13 statuses (backlog, todo,
-- planning, ready_for_dev, in_progress, code_review, fixing, testing,
-- in_review, staged, done, blocked, cancelled). `checkpoint` was a legacy
-- human-in-the-loop column that is now redundant — the staged column
-- already serves as the human-merge gate.
--
-- Safe to run idempotently. Any existing rows with status='checkpoint'
-- are migrated to 'code_review' (the closest active stage), since
-- `checkpoint` was historically reached after Quinn's review.

BEGIN;

-- (1) Migrate any existing checkpoint issues to code_review.
UPDATE issue
   SET status = 'code_review',
       updated_at = now()
 WHERE status = 'checkpoint';

-- (2) Drop and re-add the CHECK constraint without `checkpoint`.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'issue_status_check'
          AND pg_get_constraintdef(oid) LIKE '%checkpoint%'
    ) THEN
        ALTER TABLE issue DROP CONSTRAINT issue_status_check;
        ALTER TABLE issue ADD CONSTRAINT issue_status_check CHECK (
            status = ANY (ARRAY[
                'backlog','todo','in_progress','in_review','done','blocked','cancelled',
                'planning','ready_for_dev','code_review','fixing','testing','staged'
            ])
        );
    END IF;
END $$;

COMMIT;
