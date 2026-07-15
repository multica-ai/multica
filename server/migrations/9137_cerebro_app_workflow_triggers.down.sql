DROP INDEX IF EXISTS idx_cerebro_app_workflow_run_trigger_key;
ALTER TABLE cerebro_app_workflow_run
    DROP COLUMN IF EXISTS trigger_key,
    DROP COLUMN IF EXISTS trigger_type;
