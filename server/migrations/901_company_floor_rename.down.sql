DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'company_floor'::regclass AND conname = 'company_floor_name_check') THEN
        ALTER TABLE company_floor RENAME CONSTRAINT company_floor_name_check TO office_floor_name_check;
    END IF;
    IF EXISTS (SELECT 1 FROM pg_constraint WHERE conrelid = 'company_floor'::regclass AND conname = 'company_floor_pkey') THEN
        ALTER TABLE company_floor RENAME CONSTRAINT company_floor_pkey TO office_floor_pkey;
    END IF;
END $$;
ALTER INDEX IF EXISTS company_floor_workspace_position_idx RENAME TO office_floor_workspace_position_idx;
ALTER INDEX IF EXISTS company_floor_pkey_uidx RENAME TO office_floor_pkey_uidx;
ALTER TABLE IF EXISTS company_floor RENAME TO office_floor;
