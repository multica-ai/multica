-- Per-person app tool call usage for a period on the billing page: the calls
-- of one workspace counted in that period. Partial, so the many calls the
-- allowance never counted take no space in it.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_app_tool_call_workspace_metered
    ON app_tool_call (workspace_id, metered_period_start)
    WHERE metered_period_start IS NOT NULL;
