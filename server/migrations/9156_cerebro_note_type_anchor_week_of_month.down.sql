ALTER TABLE cerebro_note_type
    DROP CONSTRAINT IF EXISTS cerebro_note_type_anchor_week_of_month_check;

ALTER TABLE cerebro_note_type
    DROP COLUMN IF EXISTS anchor_week_of_month;
