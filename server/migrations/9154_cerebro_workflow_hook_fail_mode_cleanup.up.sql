-- FIR-3321: retire silent hook failures in favor of visible warnings.
UPDATE cerebro_workflow_hook_policy
SET fail_mode = 'warn'
WHERE fail_mode = 'open';

ALTER TABLE cerebro_workflow_hook_policy
    ALTER COLUMN fail_mode SET DEFAULT 'warn';

ALTER TABLE cerebro_workflow_hook_policy
    DROP CONSTRAINT IF EXISTS cerebro_workflow_hook_policy_fail_mode_check;

ALTER TABLE cerebro_workflow_hook_policy
    ADD CONSTRAINT cerebro_workflow_hook_policy_fail_mode_check
    CHECK (fail_mode IN ('closed', 'warn'));
