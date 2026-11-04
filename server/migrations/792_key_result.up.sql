-- Key results (spec 4.1): a metric at a scope, measured from start_value to
-- target_value. The progress arithmetic divides by their difference, so a key
-- result whose start and target are equal is refused here as well as in code.
--
-- No foreign keys: goal_id is validated by the service, and a goal's key results
-- are removed with it in the same transaction.
CREATE TABLE IF NOT EXISTS key_result (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    goal_id UUID NOT NULL,
    title TEXT NOT NULL CHECK (char_length(title) BETWEEN 1 AND 200),
    metric_key TEXT NOT NULL CHECK (metric_key ~ '^[a-z0-9][a-z0-9_.]{0,63}$'),
    scope_type TEXT NOT NULL CHECK (scope_type IN ('company', 'floor', 'squad', 'initiative')),
    scope_id UUID,
    start_value NUMERIC NOT NULL,
    target_value NUMERIC NOT NULL CHECK (target_value <> start_value),
    weight INTEGER NOT NULL DEFAULT 1 CHECK (weight BETWEEN 1 AND 10),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((scope_type = 'company') = (scope_id IS NULL))
);
