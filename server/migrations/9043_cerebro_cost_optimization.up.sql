-- 9043_cerebro_cost_optimization: per-workspace mode override for each agent
-- cost-saving (FIR-2325 phase 2).
--
-- The registry of savings and their default modes lives in TypeScript
-- (packages/cerebro-cost-optimization/registry.ts). This table stores ONLY the
-- per-workspace overrides — a saving whose mode differs from its registry
-- default for a given workspace. A missing row means "use the default", so
-- adding a new saving (with a default) needs no migration.
--
-- 'mode' is one of off/shadow/on (CostSavingMode in the registry):
--   off    — runs as today, no measurement.
--   shadow — runs as today, but computes what the saving WOULD have saved.
--   on     — the saving is active and we measure what it ACTUALLY saved.
--
-- Unlike cerebro_feature_flags (per-user personal toggles), cost-saving mode is
-- a workspace-level production decision: it changes agent runtime behavior for
-- every run in the workspace, so it is keyed by workspace only and writes are
-- admin-gated. updated_by records which admin last set the mode, for audit.

CREATE TABLE IF NOT EXISTS cerebro_cost_optimization (
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    saving_key TEXT NOT NULL,
    mode TEXT NOT NULL,
    updated_by UUID REFERENCES "user"(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (workspace_id, saving_key),
    CONSTRAINT cerebro_cost_optimization_mode_known
        CHECK (mode IN ('off', 'shadow', 'on'))
);
