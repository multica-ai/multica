-- FIR-3172: durable pause/resume state for interactive workflow views.
CREATE TABLE IF NOT EXISTS cerebro_app_view_request (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_run_id UUID NOT NULL REFERENCES cerebro_app_workflow_run(id) ON DELETE CASCADE,
    step_id TEXT NOT NULL,
    app_id UUID NOT NULL REFERENCES cerebro_app(id) ON DELETE CASCADE,
    app_version TEXT NOT NULL,
    view_id TEXT NOT NULL,
    input JSONB NOT NULL,
    output JSONB,
    status TEXT NOT NULL DEFAULT 'waiting' CHECK (status IN ('waiting', 'submitted')),
    submitted_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    submitted_at TIMESTAMPTZ,
    UNIQUE (workflow_run_id, step_id)
);

CREATE INDEX IF NOT EXISTS idx_cerebro_app_view_request_waiting
    ON cerebro_app_view_request(app_id, view_id, created_at DESC)
    WHERE status = 'waiting';
