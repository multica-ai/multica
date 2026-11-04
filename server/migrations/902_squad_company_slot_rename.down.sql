DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'squad'::regclass AND conname = 'squad_company_slot_check') THEN
        ALTER TABLE squad RENAME CONSTRAINT squad_company_slot_check TO squad_office_slot_check;
    END IF;
END $$;
ALTER INDEX IF EXISTS squad_company_seat_uidx RENAME TO squad_office_seat_uidx;
ALTER TABLE IF EXISTS squad RENAME COLUMN company_slot TO office_slot;
