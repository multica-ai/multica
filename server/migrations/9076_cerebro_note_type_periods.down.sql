ALTER TABLE cerebro_note_type DROP COLUMN IF EXISTS next_number;
ALTER TABLE cerebro_note_type DROP COLUMN IF EXISTS numbering_enabled;

ALTER TABLE cerebro_note_type
    DROP CONSTRAINT IF EXISTS cerebro_note_type_cadence_unit_check;
ALTER TABLE cerebro_note_type
    ADD CONSTRAINT cerebro_note_type_cadence_unit_check
    CHECK (cadence_unit IN ('manual', 'week', 'month', 'quarter'));
