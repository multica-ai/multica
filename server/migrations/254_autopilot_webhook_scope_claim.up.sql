-- Caller-supplied scope claims for webhook-triggered autopilot runs.

ALTER TABLE autopilot_run
    ADD COLUMN IF NOT EXISTS scope_claim TEXT;

ALTER TABLE webhook_delivery
    ADD COLUMN IF NOT EXISTS scope_claim TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS uq_autopilot_run_active_scope_claim
    ON autopilot_run(autopilot_id, scope_claim)
    WHERE scope_claim IS NOT NULL
      AND source = 'webhook'
      AND status IN ('pending', 'issue_created', 'running');
