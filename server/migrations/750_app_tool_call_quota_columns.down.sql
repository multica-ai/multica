ALTER TABLE app_tool_call DROP COLUMN IF EXISTS metered_period_start;

-- 'quota_exceeded' rows must be removed before rolling back; the restored
-- constraint rejects them on the next write.
ALTER TABLE app_tool_call
    DROP CONSTRAINT IF EXISTS app_tool_call_outcome_check,
    ADD CONSTRAINT app_tool_call_outcome_check
    CHECK (outcome IN ('ok', 'error', 'blocked', 'pending', 'approved', 'rejected', 'rate_limited')) NOT VALID;
