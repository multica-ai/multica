-- Per-workspace record of the dashboard time ranges people look at, so the
-- reach of the plan-based history limit (dashboard_history_days) can be
-- measured per workspace: how many workspaces read past 30 or 90 days, and how
-- many would be cut short once the limit is enforced. Metrics carry no
-- workspace label, so this cannot live in Prometheus.
--
-- Written by GET /api/dashboard/usage/by-agent, once per page load or range
-- change, only when Multica Cloud is connected. Rows aggregate per UTC day,
-- requested day count (the range the viewer picked) and returned day count
-- (what the response actually covered after the limit).
--
--   view_count               every recorded read
--   observed_over_limit_count read past the limit, admitted under a configured observe
--   degraded_over_limit_count read past the limit, admitted only because a stale
--                             cached enforce was downgraded during a Cloud outage
--   truncated_count          read past the limit, cut to the limit under enforce
--
-- The last_* columns describe the latest read. The scope unique index is built
-- concurrently in 712. No foreign keys (repo rule): workspace deletion removes
-- these rows explicitly.
CREATE TABLE IF NOT EXISTS dashboard_range_view (
    workspace_id UUID NOT NULL,
    day DATE NOT NULL,
    requested_days INTEGER NOT NULL CHECK (requested_days > 0),
    returned_days INTEGER NOT NULL CHECK (returned_days > 0),
    view_count BIGINT NOT NULL DEFAULT 0 CHECK (view_count >= 0),
    observed_over_limit_count BIGINT NOT NULL DEFAULT 0 CHECK (observed_over_limit_count >= 0),
    degraded_over_limit_count BIGINT NOT NULL DEFAULT 0 CHECK (degraded_over_limit_count >= 0),
    truncated_count BIGINT NOT NULL DEFAULT 0 CHECK (truncated_count >= 0),
    last_limit_days INTEGER,
    last_action TEXT NOT NULL,
    last_reason TEXT NOT NULL,
    first_viewed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_viewed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
