-- Dropping each constraint also drops its attached backing index. The down
-- migrations for 353 and 354 therefore become safe no-ops.
ALTER TABLE autopilot_quota_reservation
    DROP CONSTRAINT IF EXISTS autopilot_quota_reservation_pkey;

ALTER TABLE autopilot_quota_period
    DROP CONSTRAINT IF EXISTS autopilot_quota_period_pkey;
