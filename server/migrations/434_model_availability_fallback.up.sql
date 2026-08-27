ALTER TABLE model_tier_map ADD COLUMN IF NOT EXISTS fallback_concrete TEXT[] NOT NULL DEFAULT '{}';

CREATE TABLE IF NOT EXISTS model_health (
    workspace_id UUID,
    concrete TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'healthy',
    reason TEXT,
    consecutive_failures INT NOT NULL DEFAULT 0,
    last_failure_reason TEXT,
    last_failure_at TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (workspace_id, concrete)
);

CREATE TABLE IF NOT EXISTS model_pricing (
    concrete TEXT NOT NULL PRIMARY KEY,
    input_usd_per_mtok NUMERIC,
    output_usd_per_mtok NUMERIC,
    threshold_input_usd_per_mtok NUMERIC,
    fetched_at TIMESTAMPTZ
);

ALTER TABLE agent_task_queue ADD COLUMN IF NOT EXISTS requested_concrete_model TEXT;
