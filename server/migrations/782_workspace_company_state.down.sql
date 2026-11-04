ALTER TABLE workspace DROP CONSTRAINT IF EXISTS workspace_company_state_check;
ALTER TABLE workspace DROP COLUMN IF EXISTS quiet_timezone;
ALTER TABLE workspace DROP COLUMN IF EXISTS quiet_end;
ALTER TABLE workspace DROP COLUMN IF EXISTS quiet_start;
ALTER TABLE workspace DROP COLUMN IF EXISTS quiet_enabled;
ALTER TABLE workspace DROP COLUMN IF EXISTS company_state_updated_at;
ALTER TABLE workspace DROP COLUMN IF EXISTS company_state;
