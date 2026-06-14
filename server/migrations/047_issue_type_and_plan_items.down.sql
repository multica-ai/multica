DROP INDEX IF EXISTS idx_time_entry_plan_item;
ALTER TABLE time_entry DROP COLUMN IF EXISTS plan_item_id;

DROP INDEX IF EXISTS idx_focus_sessions_plan_item;
ALTER TABLE focus_sessions DROP COLUMN IF EXISTS plan_item_id;

DROP INDEX IF EXISTS idx_plan_item_unique_plan_issue;
DROP INDEX IF EXISTS idx_plan_item_user_status;
DROP INDEX IF EXISTS idx_plan_item_issue;
DROP INDEX IF EXISTS idx_plan_item_plan_position;
DROP TABLE IF EXISTS plan_item;

ALTER TABLE daily_plan
    DROP COLUMN IF EXISTS capacity_note,
    DROP COLUMN IF EXISTS capacity_minutes,
    DROP COLUMN IF EXISTS recovery_need,
    DROP COLUMN IF EXISTS energy_note,
    DROP COLUMN IF EXISTS energy_level;

DROP INDEX IF EXISTS idx_issue_workspace_type;
ALTER TABLE issue DROP COLUMN IF EXISTS issue_type_id;

DROP INDEX IF EXISTS idx_issue_type_workspace_active;
DROP TABLE IF EXISTS issue_type;
