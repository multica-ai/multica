ALTER TABLE cerebro_group_member DROP COLUMN IF EXISTS source;
DROP INDEX IF EXISTS idx_cerebro_group_google_email;
ALTER TABLE cerebro_group DROP COLUMN IF EXISTS google_group_email;
ALTER TABLE cerebro_workspace_auth_settings DROP COLUMN IF EXISTS google_workspace_sync_enabled;
