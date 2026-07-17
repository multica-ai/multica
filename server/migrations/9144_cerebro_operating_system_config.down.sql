DROP INDEX IF EXISTS idx_cerebro_rock_goal_type;
ALTER TABLE cerebro_rock DROP COLUMN IF EXISTS goal_type_id;
DROP TABLE IF EXISTS cerebro_goal_type;
DROP TABLE IF EXISTS cerebro_os_element_setting;
ALTER TABLE cerebro_operating_period DROP COLUMN IF EXISTS unit;
