-- Durable webhook dispatch state. The HTTP handler only admits the delivery;
-- a database-leased worker owns downstream autopilot dispatch.
--
-- Keep the idempotency anchor deliberately free of a foreign key. The code
-- already validates the referenced delivery, while avoiding a new FK keeps
-- deploys and rollback independent of existing webhook_delivery cascades.
ALTER TABLE webhook_delivery
    ADD COLUMN IF NOT EXISTS available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS lease_token UUID,
    ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS dispatch_attempts INTEGER NOT NULL DEFAULT 0;

ALTER TABLE autopilot_run
    ADD COLUMN IF NOT EXISTS webhook_delivery_id UUID;
