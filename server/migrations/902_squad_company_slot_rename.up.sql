-- Renames the squad's room column and its seat index from office_slot to
-- company_slot. Renaming a column keeps its data and the partial unique index
-- that covers it, so the seat rule is unchanged.
ALTER TABLE IF EXISTS squad RENAME COLUMN office_slot TO company_slot;
ALTER INDEX IF EXISTS squad_office_seat_uidx RENAME TO squad_company_seat_uidx;
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'squad'::regclass AND conname = 'squad_office_slot_check') THEN
        ALTER TABLE squad RENAME CONSTRAINT squad_office_slot_check TO squad_company_slot_check;
    END IF;
END $$;
