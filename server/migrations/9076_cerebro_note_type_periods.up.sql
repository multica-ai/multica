-- TECH-3529 bølge 2: free periods + numbering for note types.
--   * 'day' cadence (combined with cadence_count gives "every N days").
--   * cadence_count was already present; it now also drives "every N weeks".
--   * numbering: when numbering_enabled, each materialised note is stamped with
--     an incrementing number (next_number), so reviews read "#1, #2, #3 …".
--     start value is user-chosen by setting next_number before the first run.

ALTER TABLE cerebro_note_type
    DROP CONSTRAINT IF EXISTS cerebro_note_type_cadence_unit_check;
ALTER TABLE cerebro_note_type
    ADD CONSTRAINT cerebro_note_type_cadence_unit_check
    CHECK (cadence_unit IN ('manual', 'day', 'week', 'month', 'quarter'));

ALTER TABLE cerebro_note_type
    ADD COLUMN IF NOT EXISTS numbering_enabled BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE cerebro_note_type
    ADD COLUMN IF NOT EXISTS next_number INT NOT NULL DEFAULT 1;
