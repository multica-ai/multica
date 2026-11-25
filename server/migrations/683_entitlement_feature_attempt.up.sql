-- Per-workspace record of attempts to add to a restricted paid feature's
-- configuration (create a private project, add people to it, ...). Under
-- observe the attempt was admitted and only recorded here; under enforce it
-- was rejected. Rollout reach and the impact of turning a feature on are read
-- from this table; metrics carry no workspace label.
--
-- Features have no billing period, so attempts are aggregated per UTC day.
-- degraded_count separates attempts admitted only because the cached Cloud
-- policy had gone stale (a would-be enforce downgraded to observe) from a
-- configured observe rollout. The last_* columns describe the latest attempt.
--
-- The scope unique index is built concurrently in 684. No foreign keys (repo
-- rule): workspace deletion removes these rows explicitly.
CREATE TABLE IF NOT EXISTS entitlement_feature_attempt (
    workspace_id UUID NOT NULL,
    feature TEXT NOT NULL,
    day DATE NOT NULL,
    observed_count BIGINT NOT NULL DEFAULT 0 CHECK (observed_count >= 0),
    degraded_count BIGINT NOT NULL DEFAULT 0 CHECK (degraded_count >= 0),
    enforced_count BIGINT NOT NULL DEFAULT 0 CHECK (enforced_count >= 0),
    last_operation TEXT NOT NULL,
    last_action TEXT NOT NULL,
    last_reason TEXT NOT NULL,
    last_policy_revision BIGINT NOT NULL DEFAULT 0,
    last_subscription_version BIGINT NOT NULL DEFAULT 0,
    first_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
