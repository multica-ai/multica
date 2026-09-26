ALTER TABLE issue_wakeup
 DROP COLUMN IF EXISTS paused_reason,
 DROP COLUMN IF EXISTS fire_count,
 DROP COLUMN IF EXISTS max_fires,
 DROP COLUMN IF EXISTS condition_state,
 DROP COLUMN IF EXISTS condition;
