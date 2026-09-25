ALTER TABLE autopilot_run
    DROP COLUMN IF EXISTS webhook_delivery_id;

ALTER TABLE webhook_delivery
    DROP COLUMN IF EXISTS dispatch_attempts,
    DROP COLUMN IF EXISTS lease_expires_at,
    DROP COLUMN IF EXISTS lease_token,
    DROP COLUMN IF EXISTS available_at;
