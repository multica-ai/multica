-- One planning issue per squad per period (spec 4.1, the planning cycle). The
-- row is written in the same transaction as its issue, and the unique index in
-- 797 makes a second run for the same period a no-op.
CREATE TABLE IF NOT EXISTS planning_cycle (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    period_start DATE NOT NULL,
    squad_id UUID NOT NULL,
    issue_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
