-- FIR-2380: recurring note types can anchor weekly cadence to a selected
-- weekday instead of implicitly using the note type creation day.

ALTER TABLE cerebro_note_type
    ADD COLUMN IF NOT EXISTS anchor_weekday SMALLINT;

ALTER TABLE cerebro_note_type
    DROP CONSTRAINT IF EXISTS cerebro_note_type_anchor_weekday_check;

ALTER TABLE cerebro_note_type
    ADD CONSTRAINT cerebro_note_type_anchor_weekday_check
    CHECK (anchor_weekday IS NULL OR anchor_weekday BETWEEN 1 AND 7);
