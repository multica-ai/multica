ALTER TABLE autopilot_quota_period
    DROP COLUMN IF EXISTS limit_raised_at,
    DROP COLUMN IF EXISTS rejection_notified_limit;
