DROP INDEX IF EXISTS idx_cerebro_vision_plan_section_page_column_position;

DELETE FROM cerebro_vision_plan_section WHERE section_type = 'goals';

ALTER TABLE cerebro_vision_plan_section
    DROP CONSTRAINT IF EXISTS cerebro_vision_plan_section_section_type_check,
    ADD CONSTRAINT cerebro_vision_plan_section_section_type_check
        CHECK (section_type IN ('list', 'structured', 'process'));

ALTER TABLE cerebro_vision_plan_section
    DROP COLUMN IF EXISTS column_index,
    DROP COLUMN IF EXISTS page_id;

DROP INDEX IF EXISTS idx_cerebro_vision_plan_page_workspace_position;

DROP TABLE IF EXISTS cerebro_vision_plan_page;
