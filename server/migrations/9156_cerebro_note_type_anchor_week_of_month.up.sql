-- FIR-3589 item 6: recurring note types can anchor a monthly cadence to the
-- Nth weekday of the month ("3rd Monday every month") instead of the calendar
-- day-of-month. anchor_week_of_month carries the ordinal, combined with the
-- existing anchor_weekday (ISO 1..7): 1..5 select the Nth occurrence in the
-- month; -1 selects the last occurrence.

ALTER TABLE cerebro_note_type
    ADD COLUMN IF NOT EXISTS anchor_week_of_month SMALLINT;

ALTER TABLE cerebro_note_type
    DROP CONSTRAINT IF EXISTS cerebro_note_type_anchor_week_of_month_check;

ALTER TABLE cerebro_note_type
    ADD CONSTRAINT cerebro_note_type_anchor_week_of_month_check
    CHECK (anchor_week_of_month IS NULL
           OR anchor_week_of_month BETWEEN 1 AND 5
           OR anchor_week_of_month = -1);
