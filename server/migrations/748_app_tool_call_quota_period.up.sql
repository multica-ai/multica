-- Monthly app tool call usage per workspace, for the app_tool_calls
-- entitlement gate (MUL-10047). One row per workspace and Cloud-defined
-- period; admission increments used_count in a single conditional UPDATE, so
-- the gateway never counts app_tool_call rows on the hot path. Limits and
-- period boundaries come from Cloud at runtime; nothing commercial lives here.
--
-- rejection_notified_at and rejection_notified_limits mark the period's limit
-- notices: managers hear once per limit in a period, even when the limit goes
-- back to one they were already told about.
--
-- The scope unique index is built concurrently in 749. No foreign keys (repo
-- rule): workspace deletion removes these rows explicitly.
CREATE TABLE IF NOT EXISTS app_tool_call_quota_period (
    workspace_id UUID NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    used_count BIGINT NOT NULL DEFAULT 0 CHECK (used_count >= 0),
    rejection_notified_at TIMESTAMPTZ,
    rejection_notified_limits BIGINT[] NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (period_start < period_end)
);
