-- Durable, policy-agnostic accounting for the autopilot_runs entitlement gate.
-- Limits and period boundaries are supplied by Cloud at runtime; no commercial
-- defaults or calendar assumptions live in this schema.

CREATE TABLE autopilot_quota_period (
    workspace_id UUID NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    used_count BIGINT NOT NULL DEFAULT 0 CHECK (used_count >= 0),
    reserved_count BIGINT NOT NULL DEFAULT 0 CHECK (reserved_count >= 0),
    blocked_counts JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (period_start < period_end)
);

CREATE TABLE autopilot_quota_reservation (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    policy_revision BIGINT NOT NULL,
    subscription_version BIGINT NOT NULL,
    source TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'reserved'
        CHECK (state IN ('reserved', 'consumed', 'released')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finalized_at TIMESTAMPTZ,
    CHECK (period_start < period_end)
);

ALTER TABLE autopilot_run
    ADD COLUMN quota_reservation_id UUID,
    ADD COLUMN reason_code TEXT;

ALTER TABLE webhook_delivery
    ADD COLUMN reason_code TEXT,
    ADD COLUMN replay_idempotency_key TEXT;
