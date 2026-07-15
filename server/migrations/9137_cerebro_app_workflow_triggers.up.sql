-- FIR-3172: identify and deduplicate workflow runs across all five triggers.
ALTER TABLE cerebro_app_workflow_run
    ADD COLUMN IF NOT EXISTS trigger_type TEXT NOT NULL DEFAULT 'manual'
        CHECK (trigger_type IN ('schedule', 'webhook', 'data_event', 'manual', 'chat')),
    ADD COLUMN IF NOT EXISTS trigger_key TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_cerebro_app_workflow_run_trigger_key
    ON cerebro_app_workflow_run(workflow_id, trigger_type, trigger_key)
    WHERE trigger_key <> '';
