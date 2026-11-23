-- Per-workspace record of attempts that went over an entitlement limit, for
-- every gate and both acting modes. Under observe the attempt was admitted and
-- only recorded here; under enforce it was rejected. Rollout dashboards, over-
-- limit notices and grace periods read this table instead of metrics, which
-- carry no workspace label.
--
-- One row per workspace, gate and Cloud-defined period. limit_value is the
-- latest limit seen in the period and peak_usage the highest usage an attempt
-- would have reached, so peak_usage - limit_value is how far over the limit the
-- workspace went.
--
-- The scope unique index is built concurrently in 680. No foreign keys (repo
-- rule): workspace deletion removes these rows explicitly.
CREATE TABLE IF NOT EXISTS entitlement_limit_overage (
    workspace_id UUID NOT NULL,
    gate TEXT NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    limit_value BIGINT NOT NULL CHECK (limit_value >= 0),
    peak_usage BIGINT NOT NULL CHECK (peak_usage >= 0),
    observed_count BIGINT NOT NULL DEFAULT 0 CHECK (observed_count >= 0),
    enforced_count BIGINT NOT NULL DEFAULT 0 CHECK (enforced_count >= 0),
    first_exceeded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_exceeded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (period_start < period_end)
);
