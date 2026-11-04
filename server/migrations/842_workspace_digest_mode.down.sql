ALTER TABLE workspace DROP CONSTRAINT IF EXISTS workspace_digest_mode_check;
ALTER TABLE workspace DROP COLUMN IF EXISTS digest_mode;
