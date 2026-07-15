-- Single-use approval consumption for resumable platform actions (FIR-3324).
ALTER TABLE cerebro_approval_request
    ADD COLUMN IF NOT EXISTS single_use BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS consumed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_cerebro_approval_request_consumable
    ON cerebro_approval_request (workspace_id, agent_id, capability, resource, id)
    WHERE status = 'approved' AND single_use = TRUE AND consumed_at IS NULL;
