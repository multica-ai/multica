-- 9057_cerebro_cost_optimization_holdout_pct: PER-SAVING holdout share for the
-- cost-saving A/B (FIR-2640).
--
-- Phase 5 (9045) added the holdout A/B columns to the measurement table. The
-- holdout SHARE itself — what fraction of "on" runs are deliberately withheld as
-- the control arm — was a server env var only
-- (MULTICA_SERVER_FIRTAL_GATEWAY_COST_HOLDOUT_PCT), invisible and uneditable
-- from the UI. This table makes the share a per-workspace, PER-SAVING setting an
-- admin can set from the Cost optimization page, so each saving can be put at
-- full rollout (holdout 0) or any holdout independently of the others.
--
-- Like cerebro_cost_optimization (mode overrides), this stores ONLY overrides:
-- a missing (workspace, saving) row means "use the server default" (env var,
-- else the built-in default). "Full rollout (100%)" for a saving is just
-- holdout_pct=0 for that saving (no control arm — every "on" run gets it).
--
-- Only the savings with a real runtime holdout arm (model_routing,
-- prune_tool_results) consult this; the table does not restrict saving_key so a
-- future saving with a holdout arm needs no migration.
--
-- holdout_pct is 0-100; the runtime clamps too, but the CHECK keeps bad writes
-- out of the table. updated_by records which admin last set it, for audit.

CREATE TABLE IF NOT EXISTS cerebro_cost_optimization_holdout (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    saving_key TEXT NOT NULL,
    holdout_pct INTEGER NOT NULL,
    updated_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, saving_key),
    CONSTRAINT cerebro_cost_optimization_holdout_pct_range
        CHECK (holdout_pct >= 0 AND holdout_pct <= 100)
);
