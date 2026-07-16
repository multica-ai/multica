-- Crash-safe idempotency anchor for one execution action.
CREATE TABLE hook_action_effect (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    effect_key TEXT NOT NULL,
    execution_id UUID NOT NULL,
    action_index INT NOT NULL,
    action_type TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'skipped')),
    resolved_input JSONB,
    output_type TEXT,
    output_id UUID,
    attempts INT NOT NULL DEFAULT 0,
    error_code TEXT,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

COMMENT ON TABLE hook_action_effect IS 'One durable idempotency record per hook execution action. No foreign keys.';
