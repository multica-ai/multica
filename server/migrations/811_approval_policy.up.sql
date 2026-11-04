-- Standing approvals (spec 4.4): a boss-created rule that approves a narrow
-- class of proposals from squads at the right autonomy level, so the boss sees
-- them afterwards instead of beforehand.
--
-- No foreign keys (the writer checks the workspace, the creator and the kind).
-- The forbidden classes are enforced in code, not by this table.
CREATE TABLE IF NOT EXISTS approval_policy (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    kind TEXT NOT NULL,                      -- a proposal kind; never org_change or a launch gate
    match JSONB NOT NULL,                    -- {squad_ids, floor_ids, max_cost_micros, payload_equals, tags}
    max_per_week INTEGER NOT NULL DEFAULT 3 CHECK (max_per_week BETWEEN 1 AND 50),
    valid_until DATE,
    active BOOLEAN NOT NULL DEFAULT true,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
