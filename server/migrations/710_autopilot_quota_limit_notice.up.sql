-- rejection_notified_limit: the limit the period's last rejection notice was
-- sent at. A notification policy of first_rejection_per_limit notifies again
-- once the limit differs, such as after an add-on purchase raised it.
-- Metadata-only change: nullable, no default, no rewrite, no index.
ALTER TABLE autopilot_quota_period
    ADD COLUMN IF NOT EXISTS rejection_notified_limit BIGINT;
