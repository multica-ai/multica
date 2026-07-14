-- Durable webhook dispatch state. The HTTP handler only admits the delivery;
-- a database-leased worker owns downstream autopilot dispatch.
--
-- IF NOT EXISTS makes this safe for databases that already applied the
-- briefly-shipped 175_webhook_delivery_worker identity before its numeric
-- prefix collided with 175_runtime_profile_add_deveco.
ALTER TABLE webhook_delivery
    ADD COLUMN IF NOT EXISTS available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS lease_token UUID,
    ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS dispatch_attempts INTEGER NOT NULL DEFAULT 0;

ALTER TABLE autopilot_run
    ADD COLUMN IF NOT EXISTS webhook_delivery_id UUID;
