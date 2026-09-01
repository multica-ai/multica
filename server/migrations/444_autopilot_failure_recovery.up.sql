ALTER TABLE autopilot
    ADD COLUMN resource_failure_retry_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN resource_failure_retry_delay_seconds INTEGER NOT NULL DEFAULT 1800
        CHECK (resource_failure_retry_delay_seconds >= 1800),
    ADD COLUMN failure_receipt_issue_id UUID,
    ADD COLUMN failure_receipt_marker TEXT,
    ADD CONSTRAINT autopilot_failure_receipt_pair_check CHECK (
        (failure_receipt_issue_id IS NULL AND failure_receipt_marker IS NULL)
        OR
        (failure_receipt_issue_id IS NOT NULL AND failure_receipt_marker IS NOT NULL)
    ),
    ADD CONSTRAINT autopilot_failure_receipt_marker_check CHECK (
        failure_receipt_marker IS NULL
        OR failure_receipt_marker ~ '^[a-z][a-z0-9_]{0,63}$'
    ),
    ADD CONSTRAINT autopilot_failure_recovery_run_only_check CHECK (
        execution_mode = 'run_only'
        OR (
            resource_failure_retry_enabled = FALSE
            AND failure_receipt_issue_id IS NULL
        )
    );
