-- Allow 'command' as a trigger type for CEO command-initiated workflow runs.
ALTER TABLE workflow_run DROP CONSTRAINT IF EXISTS workflow_run_trigger_type_check;
ALTER TABLE workflow_run ADD CONSTRAINT workflow_run_trigger_type_check
    CHECK (trigger_type IN ('schedule', 'manual', 'command'));
