-- Table is 548. Concurrent indexes are 549 and 550. Upstream main owns
-- 545-547 (PR auto-complete and its workspace indexes).
-- Derived plan-limit snapshots reported by a local daemon. Tokens, cookies,
-- and auth.json contents are not columns on purpose: the daemon uploads
-- only provider, window, percent, reset, plan, and collected_at.
CREATE TABLE runtime_provider_usage_snapshot (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    runtime_id UUID NOT NULL,
    provider TEXT NOT NULL,
    window_id TEXT NOT NULL DEFAULT '',
    percent_used DOUBLE PRECISION,
    resets_at TIMESTAMPTZ,
    plan_name TEXT,
    collected_at TIMESTAMPTZ NOT NULL,
    reason_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
