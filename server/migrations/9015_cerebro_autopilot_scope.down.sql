DROP INDEX IF EXISTS idx_autopilot_group;
DROP INDEX IF EXISTS idx_autopilot_owner_user;

ALTER TABLE autopilot DROP CONSTRAINT IF EXISTS autopilot_scope_consistency_check;
ALTER TABLE autopilot DROP COLUMN IF EXISTS group_id;
ALTER TABLE autopilot DROP COLUMN IF EXISTS owner_user_id;
ALTER TABLE autopilot DROP COLUMN IF EXISTS scope;
