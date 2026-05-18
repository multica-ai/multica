-- Cerebro: private autopilots and inherited issue privacy.

ALTER TABLE autopilot
    ADD COLUMN IF NOT EXISTS is_private BOOLEAN NOT NULL DEFAULT FALSE;

CREATE INDEX IF NOT EXISTS idx_autopilot_private_owner
    ON autopilot(workspace_id, created_by_id)
    WHERE is_private = TRUE AND created_by_type = 'member';
