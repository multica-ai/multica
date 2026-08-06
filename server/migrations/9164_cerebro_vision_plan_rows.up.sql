-- FIR-3589: the column count belongs to a row, not to the whole page. The
-- Vision/Traction organiser this page is modelled on has a wide row over a
-- narrow one; a single per-page column_count could never express that.
--
-- A page now stores one column count per row, and a block addresses its cell
-- as (row_index, column_index) instead of a bare column.

ALTER TABLE cerebro_vision_plan_page
    ADD COLUMN IF NOT EXISTS row_column_counts JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE cerebro_vision_plan_page
    DROP CONSTRAINT IF EXISTS cerebro_vision_plan_page_row_column_counts_is_array,
    ADD CONSTRAINT cerebro_vision_plan_page_row_column_counts_is_array
        CHECK (jsonb_typeof(row_column_counts) = 'array');

ALTER TABLE cerebro_vision_plan_section
    ADD COLUMN IF NOT EXISTS row_index INTEGER NOT NULL DEFAULT 0;

-- Every existing page becomes a single row holding the columns it already had.
-- Guarded on the old column because this migration drops it below, so a re-run
-- would not even parse the backfill.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'cerebro_vision_plan_page' AND column_name = 'column_count'
    ) THEN
        EXECUTE $backfill$
            UPDATE cerebro_vision_plan_page
            SET row_column_counts = jsonb_build_array(column_count)
            WHERE row_column_counts = '[]'::jsonb
        $backfill$;
    END IF;
END $$;

-- A page created after the drop still needs at least one row.
UPDATE cerebro_vision_plan_page
SET row_column_counts = '[3]'::jsonb
WHERE row_column_counts = '[]'::jsonb;

ALTER TABLE cerebro_vision_plan_page
    DROP CONSTRAINT IF EXISTS cerebro_vision_plan_page_column_count_range,
    DROP COLUMN IF EXISTS column_count;

DROP INDEX IF EXISTS idx_cerebro_vision_plan_section_page_column_position;

CREATE INDEX IF NOT EXISTS idx_cerebro_vision_plan_section_page_cell_position
    ON cerebro_vision_plan_section (workspace_id, page_id, row_index, column_index, position, created_at);
