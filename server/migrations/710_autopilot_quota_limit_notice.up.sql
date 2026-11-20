-- rejection_notified_limit: the limit the period's last rejection notice was
-- sent at. A notification policy of first_rejection_per_limit notifies again
-- once the limit differs, such as after an add-on purchase raised it.
-- limit_raised_at: when this instance group last confirmed a paid add-on for
-- the workspace. An instance about to reject a run on a policy it received
-- before then reads the policy again first.
-- Metadata-only changes: nullable, no default, no rewrite, no index.
ALTER TABLE autopilot_quota_period
    ADD COLUMN IF NOT EXISTS rejection_notified_limit BIGINT,
    ADD COLUMN IF NOT EXISTS limit_raised_at TIMESTAMPTZ;
