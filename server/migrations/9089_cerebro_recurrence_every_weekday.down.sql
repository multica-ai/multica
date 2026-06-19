-- CEREBRO-PATCH(cerebro-recurrence-every-weekday): FIR-1611 — reverse of the
-- widened recurrence frequency CHECK constraint.
--
-- Restore the original constraint from migration 9083 (without
-- 'every_weekday'). Any rows that already use 'every_weekday' must be
-- migrated away first or the ADD CONSTRAINT will fail — that is intentional:
-- a rollback should not silently leave values the constraint forbids.

DO $$
DECLARE
    c text;
BEGIN
    FOR c IN
        SELECT conname
        FROM pg_constraint
        WHERE conrelid = 'cerebro_issue_recurrence'::regclass
          AND contype = 'c'
          AND pg_get_constraintdef(oid) ILIKE '%frequency%'
    LOOP
        EXECUTE format('ALTER TABLE cerebro_issue_recurrence DROP CONSTRAINT %I', c);
    END LOOP;
END $$;

ALTER TABLE cerebro_issue_recurrence
    ADD CONSTRAINT cerebro_issue_recurrence_frequency_check
    CHECK (frequency IN ('daily', 'weekly', 'monthly', 'yearly', 'days_after', 'custom'));
