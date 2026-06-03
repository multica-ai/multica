DROP INDEX IF EXISTS idx_cerebro_status_model_workspace_default;
ALTER TABLE cerebro_status_model DROP COLUMN IF EXISTS workspace_default;
