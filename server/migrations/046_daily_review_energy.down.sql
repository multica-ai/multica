ALTER TABLE daily_review
    DROP COLUMN IF EXISTS recovery_need,
    DROP COLUMN IF EXISTS energy_note,
    DROP COLUMN IF EXISTS energy_level;
