-- Attach the concurrently-built unique indexes as primary keys. The quota
-- period uses its actual lookup and locking key instead of an unused surrogate.
ALTER TABLE autopilot_quota_period
    ADD CONSTRAINT autopilot_quota_period_pkey
    PRIMARY KEY USING INDEX uq_autopilot_quota_period_scope;

ALTER TABLE autopilot_quota_reservation
    ADD CONSTRAINT autopilot_quota_reservation_pkey
    PRIMARY KEY USING INDEX autopilot_quota_reservation_pkey_uidx;
