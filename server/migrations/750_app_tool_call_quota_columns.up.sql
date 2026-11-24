-- Monthly app tool call limit (MUL-10047).
--
-- A call refused because the workspace used its allowance is recorded as
-- 'quota_exceeded'. Widening a whitelist cannot fail on existing rows, so the
-- constraint is replaced NOT VALID (no scan), the same as 699.
--
-- metered marks the calls the allowance counts: those sent to Composio,
-- whether they then succeeded or failed upstream. An approved call that fails
-- its checks before running, or a Remote MCP call, is not metered. The
-- constant default keeps the ADD COLUMN catalog-only.
ALTER TABLE app_tool_call
    DROP CONSTRAINT IF EXISTS app_tool_call_outcome_check,
    ADD CONSTRAINT app_tool_call_outcome_check
    CHECK (outcome IN ('ok', 'error', 'blocked', 'pending', 'approved', 'rejected', 'rate_limited', 'quota_exceeded')) NOT VALID;

ALTER TABLE app_tool_call
    ADD COLUMN IF NOT EXISTS metered BOOLEAN NOT NULL DEFAULT false;
