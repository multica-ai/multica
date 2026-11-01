ALTER TABLE autopilot_trigger
    ADD COLUMN execution_limit integer,
    ADD COLUMN execution_count bigint NOT NULL DEFAULT 0,
    ADD CONSTRAINT autopilot_trigger_execution_limit_check CHECK (
        execution_limit IS NULL OR (kind = 'schedule' AND execution_limit > 0)
    ),
    ADD CONSTRAINT autopilot_trigger_execution_count_check CHECK (execution_count >= 0);
