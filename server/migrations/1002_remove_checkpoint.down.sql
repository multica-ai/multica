-- 1002_remove_checkpoint.down.sql
--
-- Restores the `checkpoint` issue status. We cannot recover which issues
-- were originally on `checkpoint` (the up migration coalesced them into
-- `code_review`); this only re-adds the value to the CHECK constraint.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'issue_status_check'
          AND pg_get_constraintdef(oid) NOT LIKE '%checkpoint%'
    ) THEN
        ALTER TABLE issue DROP CONSTRAINT issue_status_check;
        ALTER TABLE issue ADD CONSTRAINT issue_status_check CHECK (
            status = ANY (ARRAY[
                'backlog','todo','in_progress','in_review','done','blocked','cancelled',
                'planning','ready_for_dev','code_review','fixing','testing','checkpoint','staged'
            ])
        );
    END IF;
END $$;
