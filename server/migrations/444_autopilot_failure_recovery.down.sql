ALTER TABLE autopilot
    DROP CONSTRAINT IF EXISTS autopilot_failure_recovery_run_only_check,
    DROP CONSTRAINT IF EXISTS autopilot_failure_receipt_marker_check,
    DROP CONSTRAINT IF EXISTS autopilot_failure_receipt_pair_check,
    DROP COLUMN IF EXISTS failure_receipt_marker,
    DROP COLUMN IF EXISTS failure_receipt_issue_id,
    DROP COLUMN IF EXISTS resource_failure_retry_delay_seconds,
    DROP COLUMN IF EXISTS resource_failure_retry_enabled;
