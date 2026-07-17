DROP INDEX IF EXISTS idx_cerebro_strategy_item_section_position;

ALTER TABLE cerebro_strategy_item
    DROP CONSTRAINT IF EXISTS cerebro_strategy_item_owner_pair_check,
    DROP CONSTRAINT IF EXISTS cerebro_strategy_item_owner_type_check,
    DROP COLUMN IF EXISTS owner_id,
    DROP COLUMN IF EXISTS owner_type,
    DROP COLUMN IF EXISTS part_label,
    DROP COLUMN IF EXISTS section_id;

DROP TABLE IF EXISTS cerebro_vision_plan_section;
