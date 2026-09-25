ALTER TABLE webhook_delivery
    DROP COLUMN IF EXISTS replay_idempotency_key,
    DROP COLUMN IF EXISTS reason_code;

ALTER TABLE autopilot_run
    DROP COLUMN IF EXISTS reason_code,
    DROP COLUMN IF EXISTS quota_reservation_id;

DROP TABLE IF EXISTS autopilot_quota_reservation;
DROP TABLE IF EXISTS autopilot_quota_period;
