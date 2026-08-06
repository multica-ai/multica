ALTER TABLE cerebro_vision_plan_page
    ADD COLUMN IF NOT EXISTS column_count INTEGER NOT NULL DEFAULT 3;

-- The restore reads the column this same migration then drops, so a retried
-- rollback would not even parse it without the guard.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'cerebro_vision_plan_page' AND column_name = 'row_column_counts'
    ) THEN
        EXECUTE $restore$
            UPDATE cerebro_vision_plan_page
            SET column_count = LEAST(3, GREATEST(1, COALESCE((row_column_counts->>0)::int, 3)))
        $restore$;
    END IF;
END $$;

ALTER TABLE cerebro_vision_plan_page
    DROP CONSTRAINT IF EXISTS cerebro_vision_plan_page_column_count_range,
    ADD CONSTRAINT cerebro_vision_plan_page_column_count_range CHECK (column_count BETWEEN 1 AND 3);

ALTER TABLE cerebro_vision_plan_page
    DROP CONSTRAINT IF EXISTS cerebro_vision_plan_page_row_column_counts_is_array,
    DROP COLUMN IF EXISTS row_column_counts;

DROP INDEX IF EXISTS idx_cerebro_vision_plan_section_page_cell_position;

ALTER TABLE cerebro_vision_plan_section
    DROP COLUMN IF EXISTS row_index;

CREATE INDEX IF NOT EXISTS idx_cerebro_vision_plan_section_page_column_position
    ON cerebro_vision_plan_section (workspace_id, page_id, column_index, position, created_at);
