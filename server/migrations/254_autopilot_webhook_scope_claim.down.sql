DROP INDEX IF EXISTS uq_autopilot_run_active_scope_claim;
ALTER TABLE webhook_delivery DROP COLUMN IF EXISTS scope_claim;
ALTER TABLE autopilot_run DROP COLUMN IF EXISTS scope_claim;
