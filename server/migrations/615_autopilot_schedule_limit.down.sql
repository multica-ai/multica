ALTER TABLE autopilot_trigger
    DROP CONSTRAINT IF EXISTS autopilot_trigger_execution_limit_check,
    DROP CONSTRAINT IF EXISTS autopilot_trigger_execution_count_check,
    DROP COLUMN IF EXISTS execution_limit,
    DROP COLUMN IF EXISTS execution_count;
