-- FIR-3496: scheduled eval runs (drift). One schedule row per eval; the
-- schedule sweeper claims rows whose next_run_at has elapsed and runs the eval.
-- Gated at runtime behind CEREBRO_EVAL_DRIFT_ENABLED (default OFF), so the table
-- is inert until an operator turns the sweeper on.

CREATE TABLE IF NOT EXISTS cerebro_eval_schedule (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    eval_id UUID NOT NULL REFERENCES cerebro_eval(id) ON DELETE CASCADE,
    schedule_expr TEXT NOT NULL
        CHECK (length(trim(schedule_expr)) > 0),
    timezone TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    next_run_at TIMESTAMPTZ,
    last_run_at TIMESTAMPTZ,
    created_by_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (eval_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_eval_schedule_due
    ON cerebro_eval_schedule(enabled, next_run_at);
