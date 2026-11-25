-- Upsert key for the app tool call quota increment and its usage read.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS uq_app_tool_call_quota_period_scope
    ON app_tool_call_quota_period (workspace_id, period_start, period_end);
