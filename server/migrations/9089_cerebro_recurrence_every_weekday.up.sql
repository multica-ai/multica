-- CEREBRO-PATCH(cerebro-recurrence-every-weekday): FIR-1611 — allow the
-- 'every_weekday' recurrence frequency at the database layer.
--
-- PR #1536 added the 'every_weekday' frequency to the Go validator
-- (ValidFrequencies), the TS types and the frequency dropdown, but the CHECK
-- constraint on cerebro_issue_recurrence.frequency (migration 9083) still only
-- allowed daily/weekly/monthly/yearly/days_after/custom. Saving an
-- "Every weekday" recurrence therefore failed the constraint and the API
-- returned 500 ("update recurrence failed"). This widens the constraint.
--
-- The original constraint was an unnamed inline column CHECK, so its name is
-- the Postgres default (cerebro_issue_recurrence_frequency_check). We drop any
-- CHECK constraint on the table whose definition references the frequency
-- column — regardless of its name — then re-add the widened constraint under
-- the canonical name. Idempotent: re-running is a no-op.

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
    CHECK (frequency IN ('daily', 'weekly', 'monthly', 'yearly', 'days_after', 'custom', 'every_weekday'));
