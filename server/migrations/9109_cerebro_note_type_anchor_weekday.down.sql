ALTER TABLE cerebro_note_type
    DROP CONSTRAINT IF EXISTS cerebro_note_type_anchor_weekday_check;

ALTER TABLE cerebro_note_type
    DROP COLUMN IF EXISTS anchor_weekday;
