-- A call refused because the workspace used its monthly app tool call
-- allowance is recorded as 'quota_exceeded' (MUL-10047). Widening a whitelist
-- cannot fail on existing rows, so the constraint is replaced NOT VALID (no
-- scan), the same as 699.
ALTER TABLE app_tool_call
    DROP CONSTRAINT IF EXISTS app_tool_call_outcome_check,
    ADD CONSTRAINT app_tool_call_outcome_check
    CHECK (outcome IN ('ok', 'error', 'blocked', 'pending', 'approved', 'rejected', 'rate_limited', 'quota_exceeded')) NOT VALID;
