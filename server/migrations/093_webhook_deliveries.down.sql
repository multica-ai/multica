DROP TABLE IF EXISTS webhook_delivery;

ALTER TABLE autopilot_trigger
    DROP COLUMN IF EXISTS signing_secret,
    DROP COLUMN IF EXISTS provider;
