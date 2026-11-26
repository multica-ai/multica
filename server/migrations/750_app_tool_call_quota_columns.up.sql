-- Monthly app tool call limit (MUL-10047).
--
-- A call refused because the workspace used its allowance is recorded as
-- 'quota_exceeded'. Widening a whitelist cannot fail on existing rows, so the
-- constraint is replaced NOT VALID (no scan), the same as 699.
--
-- metered_period_start is the start of the period the allowance counted the
-- call in, recorded when it was counted, so the per-person breakdown matches
-- the counter even for a call that finishes after the month turned. NULL for
-- every call the allowance did not count: refused or blocked ones, an approved
-- call that fails its checks before running, Remote MCP calls, and every call
-- while the gate is off. Nullable without a default, the ADD COLUMN is
-- catalog-only.
ALTER TABLE app_tool_call
    DROP CONSTRAINT IF EXISTS app_tool_call_outcome_check,
    ADD CONSTRAINT app_tool_call_outcome_check
    CHECK (outcome IN ('ok', 'error', 'blocked', 'pending', 'approved', 'rejected', 'rate_limited', 'quota_exceeded')) NOT VALID;

ALTER TABLE app_tool_call
    ADD COLUMN IF NOT EXISTS metered_period_start TIMESTAMPTZ;
